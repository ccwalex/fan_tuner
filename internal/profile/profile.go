package profile

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// LoadContext is normalized power fractions plus ambient.
type LoadContext struct {
	CPU  float64 `json:"cpu"`
	GPU0 float64 `json:"gpu0"`
	GPU1 float64 `json:"gpu1"`
}

// Knot is a committed cell reference in (x, T_amb) space.
type Knot struct {
	X     LoadContext `json:"x"`
	TAmb  float64     `json:"t_amb"`
	U     []float64     `json:"u"`      // committed duties 0..1 per group
	U0    []float64     `json:"u0"`     // BIOS seed at this knot if any
	Gen   int           `json:"gen"`    // config generation
}

// Profile persisted knot list + mode.
type Profile struct {
	RunMode  string `json:"mode"` // observe | control
	Knots    []Knot `json:"knots"`
	CurveGen int    `json:"curve_gen"`
	mu       sync.RWMutex
	path     string
}

// NewProfile loads or creates profile.json under dataDir.
func NewProfile(dataDir string) (*Profile, error) {
	p := &Profile{RunMode: "observe", path: filepath.Join(dataDir, "profile.json")}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(p.path); err == nil {
		if err := json.Unmarshal(b, p); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Save persists profile.
func (p *Profile) Save() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.path, b, 0644)
}

// SetMode observe or control.
func (p *Profile) SetMode(m string) {
	p.mu.Lock()
	p.RunMode = m
	p.mu.Unlock()
}

// Mode returns current mode.
func (p *Profile) Mode() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.RunMode
}

// KnotsCopy returns a snapshot of knots.
func (p *Profile) KnotsCopy() []Knot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Knot, len(p.Knots))
	copy(out, p.Knots)
	return out
}

// AppendKnot adds or replaces nearest knot in load+ambient space.
func (p *Profile) AppendKnot(k Knot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// replace if very close
	best := -1
	bestD := 1e9
	for i, existing := range p.Knots {
		d := dist(existing.X, k.X) + math.Abs(existing.TAmb-k.TAmb)*0.02
		if d < 0.05 && d < bestD {
			bestD = d
			best = i
		}
	}
	if best >= 0 {
		p.Knots[best] = k
		return
	}
	p.Knots = append(p.Knots, k)
}

// NearestURef returns committed u and whether a knot was found for this context.
func (p *Profile) NearestURef(x LoadContext, tAmb float64, nGroups int) ([]float64, bool) {
	knots := p.KnotsCopy()
	if len(knots) == 0 {
		return nil, false
	}
	u := IDW(x, tAmb, knots, nGroups)
	return u, true
}

// dist L2 in load space (clamped dimensions).
func dist(a, b LoadContext) float64 {
	return math.Sqrt(
		(a.CPU-b.CPU)*(a.CPU-b.CPU) +
			(a.GPU0-b.GPU0)*(a.GPU0-b.GPU0) +
			(a.GPU1-b.GPU1)*(a.GPU1-b.GPU1))
}

// IDW inverse-distance weight interpolate duties; clamp to neighbor hull if sparse.
func IDW(x LoadContext, tAmb float64, knots []Knot, nGroups int) []float64 {
	if len(knots) == 0 {
		return zeros(nGroups)
	}
	if len(knots) == 1 {
		return append([]float64(nil), knots[0].U...)
	}
	const eps = 1e-3
	wSum := 0.0
	u := make([]float64, nGroups)
	lo := make([]float64, nGroups)
	hi := make([]float64, nGroups)
	for i := range lo {
		lo[i] = 1
		hi[i] = 0
	}
	for _, k := range knots {
		d := dist(x, k.X) + math.Abs(tAmb-k.TAmb)*0.05
		w := 1 / (eps + d*d)
		wSum += w
		for i := range u {
			if i < len(k.U) {
				ui := k.U[i]
				u[i] += w * ui
				if ui < lo[i] {
					lo[i] = ui
				}
				if ui > hi[i] {
					hi[i] = ui
				}
			}
		}
	}
	if wSum > 0 {
		for i := range u {
			u[i] /= wSum
			u[i] = clamp(u[i], lo[i], hi[i])
		}
	}
	return u
}

func zeros(n int) []float64 {
	return make([]float64, n)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// MaxPWMDelta returns max absolute duty change between a and b.
func MaxPWMDelta(a, b []float64) float64 {
	m := 0.0
	for i := 0; i < len(a) && i < len(b); i++ {
		d := math.Abs(a[i] - b[i])
		if d > m {
			m = d
		}
	}
	return m
}

// Observation log line (append-only jsonl).
type Observation struct {
	X       LoadContext `json:"x"`
	TAmb    float64       `json:"t_amb"`
	U       []float64       `json:"u"`
	Temps   map[string]float64 `json:"temps"`
	RPMSum  float64           `json:"rpm_sum"`
	Trial   bool              `json:"trial"`
}