package sensors

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// raplMeter tracks energy_uj deltas for package power estimate.
type raplMeter struct {
	mu       sync.Mutex
	lastUJ   uint64
	lastTime time.Time
}

var packageRAPL = &raplMeter{}

func readRAPL() (float64, error) {
	path := "/sys/class/powercap/intel-rapl/intel-rapl:0/energy_uj"
	b, err := os.ReadFile(path)
	if err != nil {
		// try glob
		return 0, err
	}
	uj, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	packageRAPL.mu.Lock()
	defer packageRAPL.mu.Unlock()
	if packageRAPL.lastTime.IsZero() {
		packageRAPL.lastUJ = uj
		packageRAPL.lastTime = now
		return 0, nil
	}
	dt := now.Sub(packageRAPL.lastTime).Seconds()
	if dt < 0.1 {
		dt = 0.1
	}
	du := float64(uj - packageRAPL.lastUJ)
	if du < 0 {
		du = 0
	}
	w := du / dt / 1e6
	packageRAPL.lastUJ = uj
	packageRAPL.lastTime = now
	return w, nil
}