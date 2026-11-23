package sensors

import (
	"context"
	"sync"
	"time"

	"github.com/alexpy/fan_optimizer/internal/config"
	"github.com/alexpy/fan_optimizer/internal/sim"
)

// SimBackend drives the thermal plant.
type SimBackend struct {
	Groups config.GroupsConfig
	Plant  *sim.Plant
	mu     sync.Mutex
	duties []float64
	gpuFan []float64
	load   [3]float64
}

// NewSim creates simulator backend.
func NewSim(g config.GroupsConfig, seed int64) *SimBackend {
	n := len(g.Groups)
	return &SimBackend{
		Groups: g,
		Plant:  sim.NewPlant(n, seed),
		duties: make([]float64, n),
		gpuFan: []float64{0, 0},
		load:   [3]float64{0.1, 0.05, 0.05},
	}
}

// SetSimLoad for demo: cpu gpu0 gpu1 fractions.
func (s *SimBackend) SetSimLoad(cpu, g0, g1 float64) {
	s.mu.Lock()
	s.load = [3]float64{cpu, g0, g1}
	s.mu.Unlock()
	s.Plant.SetLoad(cpu, g0, g1)
}

// Read implements Backend.
func (s *SimBackend) Read(ctx context.Context) (Reading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Plant.Step(s.duties, s.gpuFan, 2.0)
	cpu, g0, g1 := s.Plant.Powers(s.Groups.TDPWatts)
	r := Reading{
		CPUW: cpu, GPU0W: g0, GPU1W: g1,
		Temps:     s.Plant.Temps(),
		TAmb:      s.Plant.AmbientC(),
		PWMDuties: append([]float64(nil), s.duties...),
		RPMs:      make([]float64, len(s.duties)),
		Time:      time.Now(),
	}
	for i, d := range s.duties {
		r.RPMs[i] = d * 1500
	}
	return r, nil
}

func (s *SimBackend) WriteGroupDuty(group int, duty float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if group >= 0 && group < len(s.duties) {
		s.duties[group] = duty
	}
	return nil
}

func (s *SimBackend) WriteGPUFan(gpu int, pct float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gpu < len(s.gpuFan) {
		s.gpuFan[gpu] = pct
	}
	return nil
}

func (s *SimBackend) ReleaseControl() error { return nil }

func (s *SimBackend) GroupCount() int { return len(s.Groups.Groups) }