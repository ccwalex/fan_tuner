package feedback

import (
	"encoding/json"
	"sync"

	"github.com/alexpy/fan_optimizer/internal/config"
)

// Store human prefs with thread-safe updates from fanctl.
type Store struct {
	mu  sync.RWMutex
	H   config.HumanConfig
	TooLoud bool
	TooHot  bool
}

// New returns default human config.
func New(h config.HumanConfig) *Store {
	return &Store{H: h}
}

// TooLoudEvent lowers noise budget.
func (s *Store) TooLoudEvent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TooLoud = true
	s.H.NoiseWeight = clamp(s.H.NoiseWeight*1.25, 0.5, 5)
	s.H.SearchAggression = clamp(s.H.SearchAggression*0.95, 0.5, 3)
}

// TooHotEvent raises search aggression and tightens effective targets.
func (s *Store) TooHotEvent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TooHot = true
	s.H.SearchAggression = clamp(s.H.SearchAggression*1.35, 0.5, 3)
	s.H.TempTargetOffsetC += 2
}

// Snapshot for daemon tick.
func (s *Store) Snapshot() (config.HumanConfig, bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.H, s.TooLoud, s.TooHot
}

// ClearFlags after handling one tick.
func (s *Store) ClearFlags() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TooLoud = false
	s.TooHot = false
}

// ApplyJSON command body {"cmd":"too-loud"} etc.
func (s *Store) ApplyJSON(line []byte) error {
	var msg struct {
		Cmd string `json:"cmd"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		return err
	}
	switch msg.Cmd {
	case "too-loud":
		s.TooLoudEvent()
	case "too-hot":
		s.TooHotEvent()
	}
	return nil
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}