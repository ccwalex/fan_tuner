package profile

import "testing"

func TestIDW(t *testing.T) {
	knots := []Knot{
		{X: LoadContext{CPU: 0, GPU0: 0, GPU1: 0}, TAmb: 22, U: []float64{0.2, 0.2}},
		{X: LoadContext{CPU: 1, GPU0: 0, GPU1: 0}, TAmb: 22, U: []float64{0.8, 0.3}},
	}
	u := IDW(LoadContext{CPU: 0.5, GPU0: 0, GPU1: 0}, 22, knots, 2)
	if u[0] < 0.2 || u[0] > 0.8 {
		t.Fatalf("interp cpu duty out of hull: %v", u)
	}
}

func TestMaxPWMDelta(t *testing.T) {
	if MaxPWMDelta([]float64{0.1, 0.5}, []float64{0.2, 0.3}) != 0.2 {
		t.Fatal("expected 0.2 max delta")
	}
}