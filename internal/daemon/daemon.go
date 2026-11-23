package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/alexpy/fan_optimizer/internal/config"
	"github.com/alexpy/fan_optimizer/internal/feedback"
	"github.com/alexpy/fan_optimizer/internal/gpucurve"
	"github.com/alexpy/fan_optimizer/internal/observe"
	"github.com/alexpy/fan_optimizer/internal/profile"
	"github.com/alexpy/fan_optimizer/internal/search"
	"github.com/alexpy/fan_optimizer/internal/sensors"
)

// Daemon runs the 0.5Hz control loop.
type Daemon struct {
	Groups   config.GroupsConfig
	GPU      config.GPUCurveConfig
	Prof     *profile.Profile
	Feedback *feedback.Store
	Backend  sensors.Backend
	GPUCtl   *gpucurve.Controller

	mu            sync.Mutex
	emaX          profile.LoadContext
	stableSince   time.Time
	stableAnchor  profile.LoadContext
	search        *search.State
	trialStart    time.Time
	uApply        []float64
	uRef          []float64
	liveTweak     []float64
	inTrial       bool
	lastUnderAt   time.Time
	obsPath       string
	seedDone      bool
	socketPath    string
}

// New constructs daemon.
func New(g config.GroupsConfig, gpu config.GPUCurveConfig, prof *profile.Profile, fb *feedback.Store, be sensors.Backend) *Daemon {
	n := be.GroupCount()
	return &Daemon{
		Groups: g, GPU: gpu, Prof: prof, Feedback: fb, Backend: be,
		GPUCtl:   gpucurve.New(gpu, 2),
		uApply:   make([]float64, n),
		uRef:     make([]float64, n),
		obsPath:  filepath.Join(g.DataDir, "observations.jsonl"),
		socketPath: g.SocketPath,
	}
}

// Run until context cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	runtime.GOMAXPROCS(1)
	interval := time.Duration(float64(time.Second) / d.Groups.TickHz)
	if interval < time.Second {
		interval = 2 * time.Second
	}
	go d.serveSocket(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = d.Backend.ReleaseControl()
			return nil
		case <-ticker.C:
			if err := d.tick(ctx); err != nil {
				_ = err
			}
		}
	}
}

func (d *Daemon) tick(ctx context.Context) error {
	read, err := d.Backend.Read(ctx)
	if err != nil {
		return err
	}
	x := sensors.ContextFromReading(read, d.Groups.TDPWatts)
	d.updateEMA(x)
	x = d.emaX

	human, loud, hot := d.Feedback.Snapshot()
	if loud || hot {
		d.Feedback.ClearFlags()
	}

	mode := d.Prof.Mode()
	n := d.Backend.GroupCount()

	if mode == "observe" {
		uBios := observe.BiosSeed(read.PWMDuties)
		if len(uBios) == 0 {
			uBios = make([]float64, n)
			for i := range uBios {
				uBios[i] = 0.3
			}
		}
		k := observe.FirstKnot(x, read.TAmb, uBios)
		d.Prof.AppendKnot(k)
		_ = d.Prof.Save()
		d.Prof.SetMode("control")
		copy(d.uRef, k.U0)
		copy(d.uApply, k.U0)
		d.search = search.NewState(d.uRef, human.SearchAggression, time.Now().UnixNano(), search.TrialExplore)
		d.inTrial = true
		d.trialStart = time.Now()
		d.search.Active = true
		return nil
	}

	// hard cap
	if sensors.OverHardCap(read.Temps, d.Groups.TempHardCapC) {
		for i := 0; i < n; i++ {
			_ = d.Backend.WriteGroupDuty(i, 1)
		}
		return nil
	}

	uInterp, ok := d.Prof.NearestURef(x, read.TAmb, n)
	if !ok || len(uInterp) < n {
		uInterp = append([]float64(nil), d.uRef...)
	}

	gpuTemps := []float64{
		read.Temps["gpu0"],
		read.Temps["gpu1"],
	}
	dt := 1.0 / d.Groups.TickHz
	if duties := d.GPUCtl.Tick(gpuTemps, dt); duties != nil {
		for i, pct := range duties {
			_ = d.Backend.WriteGPUFan(i, pct)
		}
	}

	stable := d.loadStable(x)
	miss := sensors.MissTarget(read.Temps, d.Groups.TempTargetsC, human.TempTargetOffsetC)
	under := sensors.UnderTarget(read.Temps, d.Groups.TempTargetsC, human.TempTargetOffsetC, d.Groups.UndercoolMarginC)
	settleDur := time.Duration(d.Groups.SettleSec * float64(time.Second))
	underCooldown := time.Since(d.lastUnderAt) >= settleDur
	newCell := d.farFromKnots(x, read.TAmb)
	ambientDrift := d.ambientDrift(x, read.TAmb)
	underTrial := under && underCooldown && !newCell && !ambientDrift && !loud

	needSearch := d.inTrial || !d.seedDone ||
		(stable && (miss || underTrial || newCell || ambientDrift || loud || hot))

	if needSearch && stable && d.search == nil {
		base := uInterp
		if miss && len(d.uRef) == n {
			base = d.uRef
		}
		mode := search.TrialExplore
		if miss || hot {
			mode = search.TrialOver
		} else if underTrial {
			mode = search.TrialUnder
		}
		d.search = search.NewState(base, human.SearchAggression, time.Now().UnixNano(), mode)
		d.inTrial = true
		d.trialStart = time.Now()
	}

	if d.inTrial && d.search != nil && stable {
		if d.search.Mode == search.TrialUnder && miss {
			d.inTrial = false
			d.search = nil
			d.lastUnderAt = time.Now()
		} else {
			over := miss || hot
			candidate := d.search.SuggestTrial()
			for i := 0; i < n; i++ {
				_ = d.Backend.WriteGroupDuty(i, candidate[i])
			}
			d.uApply = candidate
			if time.Since(d.trialStart) >= settleDur {
				o := profile.Observation{
					X: x, TAmb: read.TAmb, U: candidate, Temps: read.Temps,
					RPMSum: d.rpmSum(read), Trial: true,
				}
				_ = sensors.AppendObs(d.obsPath, o)
				os, _ := search.Score(read.Temps, d.Groups.TempTargetsC, read.TAmb, human.NoiseWeight, o.RPMSum, human.SearchAggression)
				uRef := d.cellRef(x, read.TAmb, uInterp)
				commit := false
				switch d.search.Mode {
				case search.TrialUnder:
					commit = search.ShouldCommitUnder(uRef, candidate, d.Groups.CommitPWMThresh, os, miss)
					d.lastUnderAt = time.Now()
				default:
					commit = search.ShouldCommit(uRef, candidate, d.Groups.CommitPWMThresh) && (os == 0 || !over)
				}
				if commit {
					k := profile.Knot{X: x, TAmb: read.TAmb, U: append([]float64(nil), candidate...), U0: append([]float64(nil), uRef...)}
					d.Prof.AppendKnot(k)
					_ = d.Prof.Save()
					copy(d.uRef, candidate)
				}
				d.inTrial = false
				d.search = nil
				d.seedDone = true
			}
			return nil
		}
	}

	// exploit: interpolate + slew
	target := uInterp
	if len(d.liveTweak) == n {
		target = d.liveTweak
	}
	for i := 0; i < n; i++ {
		d.uApply[i] = slew(d.uApply[i], target[i], 0.08)
		_ = d.Backend.WriteGroupDuty(i, d.uApply[i])
	}
	return nil
}

func slew(cur, tgt, step float64) float64 {
	if tgt-cur > step {
		return cur + step
	}
	if cur-tgt > step {
		return cur - step
	}
	return tgt
}

func (d *Daemon) updateEMA(x profile.LoadContext) {
	const a = 0.35
	d.emaX.CPU = a*x.CPU + (1-a)*d.emaX.CPU
	d.emaX.GPU0 = a*x.GPU0 + (1-a)*d.emaX.GPU0
	d.emaX.GPU1 = a*x.GPU1 + (1-a)*d.emaX.GPU1
}

func (d *Daemon) loadStable(x profile.LoadContext) bool {
	dist := math.Sqrt((x.CPU-d.stableAnchor.CPU)*(x.CPU-d.stableAnchor.CPU) +
		(x.GPU0-d.stableAnchor.GPU0)*(x.GPU0-d.stableAnchor.GPU0) +
		(x.GPU1-d.stableAnchor.GPU1)*(x.GPU1-d.stableAnchor.GPU1))
	if dist > 0.08 {
		d.stableAnchor = x
		d.stableSince = time.Now()
		return false
	}
	return time.Since(d.stableSince) >= time.Duration(d.Groups.StableSec*float64(time.Second))
}

func (d *Daemon) farFromKnots(x profile.LoadContext, tAmb float64) bool {
	knots := d.Prof.KnotsCopy()
	if len(knots) == 0 {
		return true
	}
	minD := 1e9
	for _, k := range knots {
		dd := profile.LoadContext{
			CPU: math.Abs(x.CPU-k.X.CPU), GPU0: math.Abs(x.GPU0-k.X.GPU0), GPU1: math.Abs(x.GPU1-k.X.GPU1),
		}
		dist := math.Sqrt(dd.CPU*dd.CPU + dd.GPU0*dd.GPU0 + dd.GPU1*dd.GPU1)
		if math.Abs(tAmb-k.TAmb) > d.Groups.AmbientStepC {
			dist += d.Groups.LoadStep
		}
		if dist < minD {
			minD = dist
		}
	}
	return minD > d.Groups.LoadStep
}

func (d *Daemon) ambientDrift(x profile.LoadContext, tAmb float64) bool {
	knots := d.Prof.KnotsCopy()
	for _, k := range knots {
		if math.Abs(x.CPU-k.X.CPU) < 0.1 && math.Abs(x.GPU0-k.X.GPU0) < 0.1 && math.Abs(x.GPU1-k.X.GPU1) < 0.1 {
			return math.Abs(tAmb-k.TAmb) >= d.Groups.AmbientStepC
		}
	}
	return false
}

func (d *Daemon) cellRef(x profile.LoadContext, tAmb float64, fallback []float64) []float64 {
	best := fallback
	bestD := 1e9
	for _, k := range d.Prof.KnotsCopy() {
		d := math.Abs(k.X.CPU-x.CPU) + math.Abs(k.X.GPU0-x.GPU0) + math.Abs(k.X.GPU1-x.GPU1) + math.Abs(k.TAmb-tAmb)*0.05
		if d < bestD {
			bestD = d
			best = k.U
		}
	}
	return append([]float64(nil), best...)
}

func (d *Daemon) rpmSum(r sensors.Reading) float64 {
	s := 0.0
	for _, v := range r.RPMs {
		s += v
	}
	return s
}

func (d *Daemon) serveSocket(ctx context.Context) {
	_ = os.Remove(d.socketPath)
	ln, err := net.Listen("unix", d.socketPath)
	if err != nil {
		return
	}
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		go d.handleConn(conn)
	}
}

func (d *Daemon) handleConn(c net.Conn) {
	defer c.Close()
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil || n == 0 {
		return
	}
	var msg map[string]string
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
		return
	}
	switch msg["cmd"] {
	case "too-loud":
		d.Feedback.TooLoudEvent()
	case "too-hot":
		d.Feedback.TooHotEvent()
	case "observe":
		d.Prof.SetMode("observe")
		d.seedDone = false
	case "seed-done":
		d.seedDone = true
	case "reload-gpu":
		if p := msg["path"]; p != "" {
			var gc config.GPUCurveConfig
			if config.LoadJSON(p, &gc) == nil {
				d.GPU = gc
				d.GPUCtl = gpucurve.New(gc, 2)
				d.Prof.CurveGen++
				_ = d.Prof.Save()
			}
		}
	default:
		if msg["cmd"] == "sim-load" {
			if sb, ok := d.Backend.(*sensors.SimBackend); ok {
				cpu := atof(msg["cpu"])
				g0 := atof(msg["gpu0"])
				g1 := atof(msg["gpu1"])
				sb.SetSimLoad(cpu, g0, g1)
			}
		}
	}
	_, _ = c.Write([]byte(`{"ok":true}`))
}

// ApplyGPUCurve updates in-process GPU controller (from web UI).
func (d *Daemon) ApplyGPUCurve(gc config.GPUCurveConfig) {
	d.GPU = gc
	n := len(gc.GPUs)
	if n == 0 {
		n = 2
	}
	d.GPUCtl = gpucurve.New(gc, n)
	d.Prof.CurveGen++
	_ = d.Prof.Save()
}

// SetCurveGen bumps profile generation on human GPU retune.
func (d *Daemon) SetCurveGen(gen int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Prof.CurveGen = gen
	_ = d.Prof.Save()
}

// Status JSON for fanctl status.
func (d *Daemon) Status() string {
	b, _ := json.Marshal(map[string]any{
		"mode": d.Prof.Mode(),
		"knots": len(d.Prof.KnotsCopy()),
	})
	return string(b)
}

// ErrHelp for CLI.
var ErrHelp = fmt.Errorf("see -h")

func atof(s string) float64 {
	var f float64
	_, _ = fmt.Sscan(s, &f)
	return f
}