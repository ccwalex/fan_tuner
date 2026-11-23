package sensors

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alexpy/fan_optimizer/internal/config"
	"github.com/alexpy/fan_optimizer/internal/profile"
)

// Reading snapshot from one tick.
type Reading struct {
	CPUW, GPU0W, GPU1W float64
	Temps              map[string]float64
	TAmb               float64
	RPMs               []float64
	PWMDuties          []float64 // per group 0..1 if readable
	Time               time.Time
}

// Backend reads hardware or sim.
type Backend interface {
	Read(ctx context.Context) (Reading, error)
	WriteGroupDuty(group int, duty float64) error
	WriteGPUFan(gpu int, pct float64) error
	ReleaseControl() error
	GroupCount() int
}

// Linux combines sysfs + nvidia-smi.
type Linux struct {
	Groups   config.GroupsConfig
	lastGPU  time.Time
	gpuCache Reading
	mu       sync.Mutex
}

// NewLinux backend.
func NewLinux(g config.GroupsConfig) *Linux {
	return &Linux{Groups: g}
}

// Read sensors.
func (l *Linux) Read(ctx context.Context) (Reading, error) {
	r := Reading{Temps: map[string]float64{}, Time: time.Now()}
	// RAPL CPU package
	if w, err := readRAPL(); err == nil {
		r.CPUW = w
	}
	l.mu.Lock()
	if time.Since(l.lastGPU) < 2*time.Second && l.gpuCache.Time.After(time.Time{}) {
		r.GPU0W = l.gpuCache.GPU0W
		r.GPU1W = l.gpuCache.GPU1W
		if t, ok := l.gpuCache.Temps["gpu0"]; ok {
			r.Temps["gpu0"] = t
		}
		if t, ok := l.gpuCache.Temps["gpu1"]; ok {
			r.Temps["gpu1"] = t
		}
		l.mu.Unlock()
	} else {
		l.mu.Unlock()
		if err := l.fillNVIDIA(ctx, &r); err != nil {
			// optional
		}
		l.mu.Lock()
		l.gpuCache = r
		l.lastGPU = time.Now()
		l.mu.Unlock()
	}
	// hwmon temps
	r.Temps["cpu"], _ = readFirstTemp("coretemp", "Package id 0")
	if r.Temps["cpu"] == 0 {
		r.Temps["cpu"], _ = readFirstTemp("k10temp", "Tctl")
	}
	r.TAmb, _ = readFirstTemp("", "temp1")
	duties := make([]float64, len(l.Groups.Groups))
	rpms := make([]float64, len(l.Groups.Groups))
	for i, g := range l.Groups.Groups {
		if len(g.Channels) > 0 {
			ch := g.Channels[0]
			p := filepath.Join("/sys/class/hwmon", ch.Hwmon, fmt.Sprintf("pwm%d", ch.PWM))
			if b, err := os.ReadFile(p); err == nil {
				v, _ := strconv.Atoi(strings.TrimSpace(string(b)))
				duties[i] = float64(v) / 255.0
			}
			fan := filepath.Join("/sys/class/hwmon", ch.Hwmon, fmt.Sprintf("fan%d", ch.PWM))
			if b, err := os.ReadFile(fan); err == nil {
				rpms[i], _ = strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
			}
		}
	}
	r.PWMDuties = duties
	r.RPMs = rpms
	return r, nil
}

func (l *Linux) fillNVIDIA(ctx context.Context, r *Reading) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=power.draw,temperature.gpu,fan.speed",
		"--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, line := range lines {
		parts := splitCSV(line)
		if len(parts) < 2 {
			continue
		}
		w, _ := strconv.ParseFloat(parts[0], 64)
		t, _ := strconv.ParseFloat(parts[1], 64)
		key := fmt.Sprintf("gpu%d", i)
		r.Temps[key] = t
		if i == 0 {
			r.GPU0W = w
		} else if i == 1 {
			r.GPU1W = w
		}
	}
	return nil
}

func splitCSV(s string) []string {
	return strings.Split(s, ", ")
}

func readFirstTemp(hwmonPrefix, label string) (float64, error) {
	matches, _ := filepath.Glob("/sys/class/hwmon/hwmon*/name")
	for _, namePath := range matches {
		dir := filepath.Dir(namePath)
		nameB, _ := os.ReadFile(namePath)
		name := strings.TrimSpace(string(nameB))
		if hwmonPrefix != "" && !strings.Contains(name, hwmonPrefix) {
			continue
		}
		labels, _ := filepath.Glob(filepath.Join(dir, "temp*_label"))
		for _, lp := range labels {
			lb, _ := os.ReadFile(lp)
			if label != "" && !strings.Contains(string(lb), label) {
				continue
			}
			num := strings.TrimPrefix(filepath.Base(lp), "temp")
			num = strings.TrimSuffix(num, "_label")
			input := filepath.Join(dir, "temp"+num+"_input")
			b, err := os.ReadFile(input)
			if err != nil {
				continue
			}
			milli, _ := strconv.Atoi(strings.TrimSpace(string(b)))
			return float64(milli) / 1000, nil
		}
	}
	return 0, fmt.Errorf("temp not found")
}

// WriteGroupDuty writes pwm for all channels in group.
func (l *Linux) WriteGroupDuty(group int, duty float64) error {
	if group < 0 || group >= len(l.Groups.Groups) {
		return fmt.Errorf("bad group")
	}
	if duty < 0 {
		duty = 0
	}
	if duty > 1 {
		duty = 1
	}
	val := int(duty * 255)
	g := l.Groups.Groups[group]
	for _, ch := range g.Channels {
		base := filepath.Join("/sys/class/hwmon", ch.Hwmon)
		enable := filepath.Join(base, fmt.Sprintf("pwm%d_enable", ch.PWM))
		_ = os.WriteFile(enable, []byte("1"), 0644)
		pwm := filepath.Join(base, fmt.Sprintf("pwm%d", ch.PWM))
		if err := os.WriteFile(pwm, []byte(strconv.Itoa(val)), 0644); err != nil {
			return err
		}
	}
	return nil
}

// WriteGPUFan via nvidia-settings exec (optional).
func (l *Linux) WriteGPUFan(gpu int, pct float64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	args := []string{
		"-c", fmt.Sprintf(":0"),
		"-a", fmt.Sprintf("[gpu:%d]/GPUFanControlState=1", gpu),
		"-a", fmt.Sprintf("[fan:%d]/GPUTargetFanSpeed=%d", gpu, int(pct)),
	}
	cmd := exec.CommandContext(ctx, "nvidia-settings", args...)
	return cmd.Run()
}

// ReleaseControl sets pwm enable to 2 (auto) where possible.
func (l *Linux) ReleaseControl() error {
	for _, g := range l.Groups.Groups {
		for _, ch := range g.Channels {
			enable := filepath.Join("/sys/class/hwmon", ch.Hwmon, fmt.Sprintf("pwm%d_enable", ch.PWM))
			_ = os.WriteFile(enable, []byte("2"), 0644)
		}
	}
	return nil
}

func (l *Linux) GroupCount() int { return len(l.Groups.Groups) }

// ContextFromReading builds normalized load context.
func ContextFromReading(r Reading, tdp config.TDPLimits) profile.LoadContext {
	frac := func(w, tdp float64) float64 {
		if tdp <= 0 {
			return 0
		}
		f := w / tdp
		if f > 1.2 {
			f = 1.2
		}
		return f
	}
	return profile.LoadContext{
		CPU:  frac(r.CPUW, tdp.CPU),
		GPU0: frac(r.GPU0W, tdp.GPU0),
		GPU1: frac(r.GPU1W, tdp.GPU1),
	}
}

// OverHardCap checks temps.
func OverHardCap(temps map[string]float64, cap config.TempTargets) bool {
	checks := map[string]float64{"cpu": cap.CPU, "gpu0": cap.GPU0, "gpu1": cap.GPU1}
	for k, lim := range checks {
		if lim > 0 {
			if t, ok := temps[k]; ok && t >= lim {
				return true
			}
		}
	}
	return false
}

// MissTarget true if any temp above target.
func MissTarget(temps map[string]float64, targets config.TempTargets, offset float64) bool {
	checks := map[string]float64{"cpu": targets.CPU - offset, "gpu0": targets.GPU0 - offset, "gpu1": targets.GPU1 - offset}
	for k, lim := range checks {
		if lim > 0 {
			if t, ok := temps[k]; ok && t > lim {
				return true
			}
		}
	}
	return false
}

// AppendObs logs jsonl line.
func AppendObs(path string, o profile.Observation) error {
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}