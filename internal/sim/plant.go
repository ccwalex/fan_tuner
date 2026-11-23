package sim

import (
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/alexpy/fan_optimizer/internal/config"
)

// Plant is a hidden-coupling thermal model for case groups + GPUs.
type Plant struct {
	mu      sync.Mutex
	Groups  int
	Coupling [][]float64 // groups x heat sources (cpu, gpu0, gpu1)
	TChip   [3]float64
	TAmb    float64
	Load    [3]float64 // 0..1
	U       []float64
	GPUDuty []float64
	RNG     *rand.Rand
	last    time.Time
}

// NewPlant creates random coupling (hidden from optimizer).
func NewPlant(nGroups int, seed int64) *Plant {
	r := rand.New(rand.NewSource(seed))
	c := make([][]float64, nGroups)
	for g := range c {
		c[g] = []float64{r.Float64()*0.5 + 0.2, r.Float64()*0.5 + 0.2, r.Float64()*0.5 + 0.2}
	}
	return &Plant{
		Groups:   nGroups,
		Coupling: c,
		U:        make([]float64, nGroups),
		GPUDuty:  []float64{0, 0},
		TAmb:     22,
		RNG:      r,
		last:     time.Now(),
	}
}

// SetAmbient for weather/placement tests.
func (p *Plant) SetAmbient(t float64) {
	p.mu.Lock()
	p.TAmb = t
	p.mu.Unlock()
}

// SetLoad cpu gpu0 gpu1 fractions.
func (p *Plant) SetLoad(cpu, g0, g1 float64) {
	p.mu.Lock()
	p.Load = [3]float64{cpu, g0, g1}
	p.mu.Unlock()
}

// Step advances physics given duties.
func (p *Plant) Step(u []float64, gpuDuty []float64, dt float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if dt <= 0 {
		dt = 1
	}
	copy(p.U, u)
	if len(gpuDuty) >= 2 {
		p.GPUDuty[0] = gpuDuty[0] / 100
		p.GPUDuty[1] = gpuDuty[1] / 100
	}
	cool := [3]float64{}
	for g := 0; g < p.Groups && g < len(u); g++ {
		air := u[g]
		cool[0] += air * p.Coupling[g][0]
		cool[1] += air * p.Coupling[g][1]
		cool[2] += air * p.Coupling[g][2]
	}
	heat := [3]float64{p.Load[0] * 80, p.Load[1] * 120, p.Load[2] * 120}
	for i := 0; i < 3; i++ {
		gpuCool := 0.0
		if i > 0 {
			gpuCool = p.GPUDuty[i-1] * 40
		}
		dT := (heat[i] - cool[i]*25 - gpuCool - (p.TChip[i]-p.TAmb)*0.15) * dt * 0.08
		p.TChip[i] += dT
		if p.TChip[i] < p.TAmb {
			p.TChip[i] = p.TAmb
		}
	}
	p.last = time.Now()
}

// Temps map for sensors.
func (p *Plant) Temps() map[string]float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]float64{
		"cpu":  p.TChip[0],
		"gpu0": p.TChip[1],
		"gpu1": p.TChip[2],
	}
}

// Powers watts estimate.
func (p *Plant) Powers(tdp config.TDPLimits) (cpu, g0, g1 float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Load[0] * tdp.CPU, p.Load[1] * tdp.GPU0, p.Load[2] * tdp.GPU1
}

// AmbientC .
func (p *Plant) AmbientC() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.TAmb
}

// RPMSum fake noise proxy.
func (p *Plant) RPMSum() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := 0.0
	for _, u := range p.U {
		s += u * 1200
	}
	for _, d := range p.GPUDuty {
		s += d * 2000
	}
	return s
}

// Distance load from anchor for stability tests.
func LoadDist(cpu, g0, g1 float64, x [3]float64) float64 {
	return math.Sqrt((cpu-x[0])*(cpu-x[0]) + (g0-x[1])*(g0-x[1]) + (g1-x[2])*(g1-x[2]))
}