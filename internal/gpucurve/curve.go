package gpucurve

import (
	"math"

	"github.com/alexpy/fan_optimizer/internal/config"
)

// Controller applies human GPU curves with hysteresis and slew.
type Controller struct {
	Cfg    config.GPUCurveConfig
	Duty   []float64 // current % 0..100 per GPU
	On     []bool
}

// New builds state for n GPUs from config.
func New(cfg config.GPUCurveConfig, n int) *Controller {
	if n == 0 {
		n = len(cfg.GPUs)
	}
	c := &Controller{
		Cfg:  cfg,
		Duty: make([]float64, n),
		On:   make([]bool, n),
	}
	return c
}

// TargetDuty computes desired fan % from GPU temp (curve mode).
func TargetDuty(p config.GPUParams, tempC float64) float64 {
	if p.TOff == 0 && p.TOn > 0 {
		p.TOff = p.TOn - 3
	}
	on := p.TOn
	off := p.TOff
	if tempC >= on {
		tFull := p.TFull
		if tFull <= on {
			tFull = on + 30
		}
		frac := (tempC - on) / (tFull - on)
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		d := p.Scale * 100 * frac
		if d < p.MinDuty {
			d = p.MinDuty
		}
		if d > 100 {
			d = 100
		}
		return d
	}
	if tempC <= off {
		return 0
	}
	// hysteresis band: hold last implied min
	return 0
}

// Tick updates duties from GPU temps; returns duties to apply (nil = auto).
func (c *Controller) Tick(temps []float64, dt float64) []float64 {
	if c.Cfg.Mode != "curve" {
		return nil
	}
	out := make([]float64, len(temps))
	for i := range temps {
		p := c.paramsFor(i)
		tCrit := c.Cfg.TCritC
		if tCrit == 0 {
			tCrit = 83
		}
		target := TargetDuty(p, temps[i])
		if temps[i] >= tCrit {
			target = 100
		}
		if temps[i] >= p.TOn {
			c.On[i] = true
		}
		if temps[i] <= p.TOff {
			c.On[i] = false
			target = 0
		}
		if c.On[i] && target == 0 && temps[i] > p.TOff {
			target = p.MinDuty
		}
		slew := p.SlewDown
		if target > c.Duty[i] {
			slew = p.SlewUp
		}
		delta := target - c.Duty[i]
		maxStep := slew * dt
		if delta > maxStep {
			delta = maxStep
		}
		if delta < -maxStep {
			delta = -maxStep
		}
		c.Duty[i] += delta
		c.Duty[i] = clamp(c.Duty[i], 0, 100)
		out[i] = c.Duty[i]
	}
	return out
}

func (c *Controller) paramsFor(i int) config.GPUParams {
	for _, p := range c.Cfg.GPUs {
		if p.Index == i {
			return p
		}
	}
	return config.GPUParams{
		TOn: 45, TOff: 42, MinDuty: 30, TFull: 75, Scale: 1,
		SlewUp: 8, SlewDown: 4,
	}
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}