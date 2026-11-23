package gpucurve

import (
	"testing"

	"github.com/alexpy/fan_optimizer/internal/config"
)

func TestHysteresis(t *testing.T) {
	c := New(config.GPUCurveConfig{
		Mode: "curve",
		GPUs: []config.GPUParams{{
			TOn: 50, TOff: 47, MinDuty: 30, TFull: 80, SlewUp: 100, SlewDown: 100,
		}},
	}, 1)
	d := c.Tick([]float64{55}, 1)
	if d == nil || d[0] < 30 {
		t.Fatalf("expected fan on at 55C, got %v", d)
	}
}