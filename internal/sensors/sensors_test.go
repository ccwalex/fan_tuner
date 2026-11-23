package sensors

import (
	"testing"

	"github.com/alexpy/fan_optimizer/internal/config"
)

func TestUnderTarget(t *testing.T) {
	targets := config.TempTargets{CPU: 75, GPU0: 78, GPU1: 0}
	t.Run("all below margin", func(t *testing.T) {
		temps := map[string]float64{"cpu": 70, "gpu0": 73}
		if !UnderTarget(temps, targets, 0, 5) {
			t.Fatal("expected under")
		}
	})
	t.Run("one at margin boundary", func(t *testing.T) {
		temps := map[string]float64{"cpu": 70, "gpu0": 74}
		if UnderTarget(temps, targets, 0, 5) {
			t.Fatal("73 margin needs <=73; 74 is not under")
		}
	})
	t.Run("offset shrinks effective target", func(t *testing.T) {
		temps := map[string]float64{"cpu": 68, "gpu0": 71}
		if !UnderTarget(temps, targets, 2, 5) {
			t.Fatal("expected under with offset")
		}
	})
	t.Run("missing sensor", func(t *testing.T) {
		temps := map[string]float64{"cpu": 60}
		if UnderTarget(temps, targets, 0, 5) {
			t.Fatal("gpu0 required")
		}
	})
	t.Run("zero margin", func(t *testing.T) {
		temps := map[string]float64{"cpu": 50, "gpu0": 50}
		if UnderTarget(temps, targets, 0, 0) {
			t.Fatal("margin 0 disables under")
		}
	})
	t.Run("ignores zero target gpu1", func(t *testing.T) {
		temps := map[string]float64{"cpu": 70, "gpu0": 73}
		if !UnderTarget(temps, targets, 0, 5) {
			t.Fatal("gpu1 target 0 should be ignored")
		}
	})
}