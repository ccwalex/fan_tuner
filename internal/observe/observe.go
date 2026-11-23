package observe

import (
	"github.com/alexpy/fan_optimizer/internal/profile"
)

// BiosSeed returns duties read from firmware (already 0..1 per group).
func BiosSeed(readDuties []float64) []float64 {
	out := make([]float64, len(readDuties))
	copy(out, readDuties)
	return out
}

// FirstKnot from BIOS at start context.
func FirstKnot(x profile.LoadContext, tAmb float64, uBios []float64) profile.Knot {
	u0 := BiosSeed(uBios)
	return profile.Knot{
		X: x, TAmb: tAmb,
		U: append([]float64(nil), u0...),
		U0: append([]float64(nil), u0...),
	}
}