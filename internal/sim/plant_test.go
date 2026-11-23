package sim

import (
	"testing"

	"github.com/alexpy/fan_optimizer/internal/config"
)

func TestAmbientDriftHeatsChips(t *testing.T) {
	p := NewPlant(3, 1)
	p.SetAmbient(22)
	p.SetLoad(0.8, 0.9, 0.1)
	u := []float64{0.2, 0.2, 0.2}
	for i := 0; i < 30; i++ {
		p.Step(u, []float64{30, 0}, 2)
	}
	t22 := p.Temps()["cpu"]
	p.SetAmbient(32)
	for i := 0; i < 30; i++ {
		p.Step(u, []float64{30, 0}, 2)
	}
	t32 := p.Temps()["cpu"]
	if t32 <= t22 {
		t.Fatalf("expected hotter chips at higher ambient: %v vs %v", t32, t22)
	}
	_ = config.GroupsConfig{}
}