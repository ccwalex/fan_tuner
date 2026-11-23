package search

import (
	"math"
	"math/rand"

	"github.com/alexpy/fan_optimizer/internal/config"
	"github.com/alexpy/fan_optimizer/internal/profile"
)

// State tracks an in-progress trial.
type State struct {
	Active     bool
	URef       []float64
	UCandidate []float64
	OverTarget bool
	Aggression float64
	RNG        *rand.Rand
}

// NewState starts search around uRef.
func NewState(uRef []float64, aggression float64, seed int64) *State {
	u := append([]float64(nil), uRef...)
	return &State{
		URef:       uRef,
		UCandidate: u,
		Aggression: aggression,
		RNG:        rand.New(rand.NewSource(seed)),
	}
}

// SuggestTrial proposes next differential or scale move.
func (s *State) SuggestTrial(overTarget bool) []float64 {
	n := len(s.URef)
	out := append([]float64(nil), s.UCandidate...)
	step := 0.05 * s.Aggression
	if overTarget {
		if s.RNG.Float64() < 0.4 {
			for i := range out {
				out[i] = clamp01(out[i] + step*2)
			}
		} else {
			g := s.RNG.Intn(n)
			out[g] = clamp01(out[g] + step*3)
		}
	} else {
		switch s.RNG.Intn(3) {
		case 0:
			g := s.RNG.Intn(n)
			out[g] = clamp01(out[g] + s.RNG.NormFloat64()*step)
		case 1:
			g1 := s.RNG.Intn(n)
			g2 := s.RNG.Intn(n)
			if g1 != g2 {
				d := step * 2
				out[g1] = clamp01(out[g1] + d)
				out[g2] = clamp01(out[g2] - d*0.5)
			}
		default:
			for i := range out {
				out[i] = clamp01(out[i] - step*0.5)
			}
		}
	}
	s.UCandidate = out
	return out
}

// Score evaluates trial temps vs targets (lower is better). Uses chip-ambient if tAmb>0.
func Score(temps map[string]float64, targets config.TempTargets, tAmb, noiseWeight, rpmSum float64, agg float64) (overshoot float64, quiet float64) {
	effective := config.TempTargets{
		CPU:  targets.CPU - agg,
		GPU0: targets.GPU0 - agg,
		GPU1: targets.GPU1 - agg,
	}
	checks := map[string]float64{"cpu": effective.CPU, "gpu0": effective.GPU0, "gpu1": effective.GPU1}
	for key, limit := range checks {
		if tv, ok := temps[key]; ok {
			v := tv
			if tAmb > 0 {
				v = tv - tAmb*0.5
				limit -= tAmb * 0.1
			}
			if v > limit {
				overshoot += v - limit
			}
		}
	}
	quiet = noiseWeight * rpmSum / 1000
	return overshoot, quiet
}

// ShouldCommit if candidate moved enough from uRef.
func ShouldCommit(uRef, candidate []float64, thresh float64) bool {
	return profile.MaxPWMDelta(uRef, candidate) >= thresh
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}