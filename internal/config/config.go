// Package config holds JSON-serializable configuration (stdlib only).
package config

import (
	"encoding/json"
	"os"
)

// GroupPWM maps one logical fan group to hwmon PWM channels.
type GroupPWM struct {
	Name    string   `json:"name"`
	Channels []PWMChan `json:"channels"`
}

// PWMChan is a sysfs pwm path fragment: hwmon name + pwm index (1-based).
type PWMChan struct {
	Hwmon string `json:"hwmon"` // e.g. "hwmon2"
	PWM   int    `json:"pwm"`   // 1..n
}

// GroupsConfig defines case/CPU bundles and thermal targets.
type GroupsConfig struct {
	Groups          []GroupPWM         `json:"groups"`
	TDPWatts        TDPLimits          `json:"tdp_watts"`
	TempTargetsC    TempTargets        `json:"temp_targets_c"`
	TempHardCapC    TempTargets        `json:"temp_hard_cap_c"`
	CommitPWMThresh float64            `json:"commit_pwm_threshold"` // default 0.10
	LoadStep        float64            `json:"load_step"`            // power frac step
	AmbientStepC    float64            `json:"ambient_step_c"`
	StableSec       float64            `json:"stable_sec"`
	SettleSec       float64            `json:"settle_sec"`
	TickHz          float64            `json:"tick_hz"`
	SocketPath         string             `json:"socket_path"`
	DataDir            string             `json:"data_dir"`
	UndercoolMarginC   float64            `json:"undercool_margin_c"` // default 5: step-down when all temps this far below target
}

// TDPLimits for normalizing power context.
type TDPLimits struct {
	CPU  float64 `json:"cpu"`
	GPU0 float64 `json:"gpu0"`
	GPU1 float64 `json:"gpu1"`
}

// TempTargets per sensor name: cpu, gpu0, gpu1.
type TempTargets struct {
	CPU  float64 `json:"cpu"`
	GPU0 float64 `json:"gpu0"`
	GPU1 float64 `json:"gpu1"`
}

// HumanConfig loudness vs aggressiveness from feedback.
type HumanConfig struct {
	NoiseWeight       float64 `json:"noise_weight"`
	SearchAggression  float64 `json:"search_aggression"` // 1 = default
	TempTargetOffsetC float64 `json:"temp_target_offset_c"`
}

// GPUCurveConfig per-GPU human curve (not learned).
type GPUCurveConfig struct {
	Mode      string      `json:"mode"` // "nvidia-auto" | "curve"
	GPUs      []GPUParams `json:"gpus"`
	TCritC    float64     `json:"t_crit_c"`
}

// GPUParams human knobs.
type GPUParams struct {
	Index      int     `json:"index"`
	TOn        float64 `json:"t_on"`
	TOff       float64 `json:"t_off"`
	MinDuty    float64 `json:"min_duty"`
	TFull      float64 `json:"t_full"`
	Scale      float64 `json:"scale"`
	SlewUp     float64 `json:"slew_up"`
	SlewDown   float64 `json:"slew_down"`
	CurveGen   int     `json:"curve_gen"` // bumps on human retune
}

// LoadJSON reads a JSON file into v.
func LoadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// SaveJSON writes v as indented JSON.
func SaveJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

// Defaults fills zero fields on GroupsConfig.
func (g *GroupsConfig) Defaults() {
	if g.CommitPWMThresh == 0 {
		g.CommitPWMThresh = 0.10
	}
	if g.LoadStep == 0 {
		g.LoadStep = 0.15
	}
	if g.AmbientStepC == 0 {
		g.AmbientStepC = 4
	}
	if g.StableSec == 0 {
		g.StableSec = 8
	}
	if g.SettleSec == 0 {
		g.SettleSec = 45
	}
	if g.TickHz == 0 {
		g.TickHz = 0.5
	}
	if g.SocketPath == "" {
		g.SocketPath = "/run/fan-daemon.sock"
	}
	if g.DataDir == "" {
		g.DataDir = "data"
	}
	if g.TDPWatts.CPU == 0 {
		g.TDPWatts.CPU = 250
	}
	if g.TDPWatts.GPU0 == 0 {
		g.TDPWatts.GPU0 = 450
	}
	if g.TDPWatts.GPU1 == 0 {
		g.TDPWatts.GPU1 = 450
	}
	if g.UndercoolMarginC == 0 {
		g.UndercoolMarginC = 5
	}
}