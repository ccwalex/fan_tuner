package discover

import (
	"testing"

	"github.com/alexpy/fan_optimizer/internal/config"
)

func TestGroupName(t *testing.T) {
	n := groupName(PWMHeader{HwmonName: "nct6775", Label: "CPU Fan", PWM: 1})
	if n == "" {
		t.Fatal("empty name")
	}
}

func TestOneGroupPerHeader(t *testing.T) {
	hs := []PWMHeader{
		{Hwmon: "hwmon2", HwmonName: "ec", PWM: 1},
		{Hwmon: "hwmon2", HwmonName: "ec", PWM: 2},
	}
	g := GroupsFromHeaders(hs)
	if len(g) != 2 {
		t.Fatalf("expected 2 groups for 2 pwm, got %d", len(g))
	}
	if len(g[0].Channels) != 1 {
		t.Fatal("daisy chain is one pwm per group")
	}
}

func TestMergeIdentical(t *testing.T) {
	hs := []PWMHeader{
		{Hwmon: "hwmon2", HwmonName: "ec", PWM: 1},
		{Hwmon: "hwmon2", HwmonName: "ec", PWM: 2},
	}
	duty := map[string]int{"hwmon2:1": 128, "hwmon2:2": 128}
	g := MergeIdenticalPWM(hs, duty)
	if len(g) != 1 {
		t.Fatalf("expected merged group, got %d", len(g))
	}
	if len(g[0].Channels) != 2 {
		t.Fatalf("expected 2 channels in merged group")
	}
	_ = config.GroupsConfig{}
}