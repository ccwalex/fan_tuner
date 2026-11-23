// Package discover scans Linux hwmon for PWM headers (one group per header).
// Fans on a daisy-chain / splitter share one PWM; they appear as a single group.
package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/alexpy/fan_optimizer/internal/config"
)

// PWMHeader is one controllable fan output.
type PWMHeader struct {
	SysfsDir string // e.g. /sys/class/hwmon/hwmon2
	Hwmon    string // hwmon2
	HwmonName string
	PWM      int
	FanRPM   int // fanN with same index if present, else 0
	Label    string
}

// ScanPWM walks /sys/class/hwmon and returns one entry per pwmN control file.
func ScanPWM() ([]PWMHeader, error) {
	dirs, err := filepath.Glob("/sys/class/hwmon/hwmon*")
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	var out []PWMHeader
	for _, dir := range dirs {
		base := filepath.Base(dir)
		name := readTrim(filepath.Join(dir, "name"))
		matches, _ := filepath.Glob(filepath.Join(dir, "pwm[0-9]*"))
		for _, p := range matches {
			baseName := filepath.Base(p)
			if strings.HasSuffix(baseName, "_enable") || strings.HasSuffix(baseName, "_mode") {
				continue
			}
			if !strings.HasPrefix(baseName, "pwm") {
				continue
			}
			idxStr := strings.TrimPrefix(baseName, "pwm")
			idx, err := strconv.Atoi(idxStr)
			if err != nil || idx < 1 {
				continue
			}
			// readable pwm duty
			if _, err := os.ReadFile(p); err != nil {
				continue
			}
			label := pwmLabel(dir, idx)
			rpm := readFanRPM(dir, idx)
			out = append(out, PWMHeader{
				SysfsDir: dir, Hwmon: base, HwmonName: name,
				PWM: idx, FanRPM: rpm, Label: label,
			})
		}
	}
	return out, nil
}

func pwmLabel(hwmonDir string, pwm int) string {
	// some boards expose pwmN_label
	if b, err := os.ReadFile(filepath.Join(hwmonDir, fmt.Sprintf("pwm%d_label", pwm))); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

func readFanRPM(hwmonDir string, idx int) int {
	f := filepath.Join(hwmonDir, fmt.Sprintf("fan%d_input", idx))
	b, err := os.ReadFile(f)
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return v
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// GroupsFromHeaders builds one optimizer group per PWM header (daisy-chain = one header).
func GroupsFromHeaders(headers []PWMHeader) []config.GroupPWM {
	out := make([]config.GroupPWM, 0, len(headers))
	for _, h := range headers {
		name := groupName(h)
		out = append(out, config.GroupPWM{
			Name: name,
			Channels: []config.PWMChan{{
				Hwmon: h.Hwmon,
				PWM:   h.PWM,
			}},
		})
	}
	return out
}

func groupName(h PWMHeader) string {
	parts := []string{h.HwmonName}
	if h.Label != "" {
		parts = append(parts, slug(h.Label))
	} else {
		parts = append(parts, fmt.Sprintf("pwm%d", h.PWM))
	}
	return strings.Join(parts, "_")
}

func slug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '/' {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// MergeIdenticalPWM suggests merging headers that always share the same duty
// (e.g. BIOS sync). samples: map hwmon:pwm -> duty 0..255 read twice externally.
// Not used by default — single physical daisy chain is already one pwm file.
func MergeIdenticalPWM(headers []PWMHeader, duty map[string]int) []config.GroupPWM {
	if len(duty) == 0 {
		return GroupsFromHeaders(headers)
	}
	key := func(h PWMHeader) string { return fmt.Sprintf("%s:%d", h.Hwmon, h.PWM) }
	inv := make(map[int][]PWMHeader)
	for _, h := range headers {
		d, ok := duty[key(h)]
		if !ok {
			return GroupsFromHeaders(headers)
		}
		inv[d] = append(inv[d], h)
	}
	var groups []config.GroupPWM
	gi := 0
	for _, hs := range inv {
		if len(hs) == 0 {
			continue
		}
		chs := make([]config.PWMChan, len(hs))
		for i, h := range hs {
			chs[i] = config.PWMChan{Hwmon: h.Hwmon, PWM: h.PWM}
		}
		name := fmt.Sprintf("synced_%d", gi)
		gi++
		if len(hs) == 1 {
			name = groupName(hs[0])
		}
		groups = append(groups, config.GroupPWM{Name: name, Channels: chs})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return groups
}

// DefaultGroupsConfig returns discover-derived groups with sane defaults.
func DefaultGroupsConfig(headers []PWMHeader) config.GroupsConfig {
	g := config.GroupsConfig{
		Groups: GroupsFromHeaders(headers),
	}
	g.Defaults()
	return g
}