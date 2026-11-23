package webui

import (
	"context"
	_ "embed"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/alexpy/fan_optimizer/internal/config"
)

//go:embed page.html
var pageHTML []byte

// Server serves GPU curve editor on localhost.
type Server struct {
	Addr       string
	GPUPath    string
	SocketPath string
	OnApply    func(cfg config.GPUCurveConfig)
}

// Run starts HTTP server until ctx done.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(pageHTML)
	})
	mux.HandleFunc("/api/gpu-curve", s.handleGPUCurve)

	srv := &http.Server{Addr: s.Addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sh, _ := context.WithTimeout(context.Background(), 3*time.Second)
		_ = srv.Shutdown(sh)
	}()
	return srv.ListenAndServe()
}

func (s *Server) handleGPUCurve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		var cfg config.GPUCurveConfig
		if err := config.LoadJSON(s.GPUPath, &cfg); err != nil {
			cfg = config.GPUCurveConfig{Mode: "curve", TCritC: 83, GPUs: []config.GPUParams{{
				Index: 0, TOn: 45, TOff: 42, MinDuty: 30, TFull: 75, Scale: 1, SlewUp: 8, SlewDown: 4,
			}}}
		}
		_ = json.NewEncoder(w).Encode(cfg)
	case http.MethodPost:
		var cfg config.GPUCurveConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		for i := range cfg.GPUs {
			cfg.GPUs[i].CurveGen++
		}
		if err := config.SaveJSON(s.GPUPath, cfg); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if s.OnApply != nil {
			s.OnApply(cfg)
		}
		if s.SocketPath != "" {
			_ = notifyDaemon(s.SocketPath, s.GPUPath)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func notifyDaemon(socketPath, gpuPath string) error {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	msg, _ := json.Marshal(map[string]string{"cmd": "reload-gpu", "path": gpuPath})
	_, err = conn.Write(msg)
	return err
}