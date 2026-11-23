package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexpy/fan_optimizer/internal/config"
	"github.com/alexpy/fan_optimizer/internal/daemon"
	"github.com/alexpy/fan_optimizer/internal/feedback"
	"github.com/alexpy/fan_optimizer/internal/profile"
	"github.com/alexpy/fan_optimizer/internal/sensors"
	"github.com/alexpy/fan_optimizer/internal/webui"
)

func main() {
	groupsPath := flag.String("groups", "config/groups.example.json", "groups config JSON")
	gpuPath := flag.String("gpu", "config/gpu_curve.json", "GPU curve JSON")
	humanPath := flag.String("human", "config/human.json", "human prefs JSON")
	sim := flag.Bool("sim", false, "simulator backend (Mac / dev)")
	seed := flag.Int64("sim-seed", 42, "sim RNG seed")
	webAddr := flag.String("web", "", "if set, serve GPU curve UI on this address (e.g. 127.0.0.1:8765)")
	flag.Parse()

	var groups config.GroupsConfig
	if err := config.LoadJSON(*groupsPath, &groups); err != nil {
		fmt.Fprintln(os.Stderr, "groups:", err)
		os.Exit(1)
	}
	groups.Defaults()

	var gpu config.GPUCurveConfig
	_ = config.LoadJSON(*gpuPath, &gpu)

	var human config.HumanConfig
	_ = config.LoadJSON(*humanPath, &human)

	prof, err := profile.NewProfile(groups.DataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(prof.KnotsCopy()) > 0 {
		prof.SetMode("control")
	}

	var be sensors.Backend
	if *sim {
		be = sensors.NewSim(groups, *seed)
	} else {
		be = sensors.NewLinux(groups)
	}

	d := daemon.New(groups, gpu, prof, feedback.New(human), be)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if *webAddr != "" {
		srv := &webui.Server{
			Addr:       *webAddr,
			GPUPath:    *gpuPath,
			SocketPath: groups.SocketPath,
			OnApply: func(gc config.GPUCurveConfig) {
				d.ApplyGPUCurve(gc)
			},
		}
		go func() {
			fmt.Println("GPU curve UI:", "http://"+*webAddr)
			_ = srv.Run(ctx)
		}()
	}
	fmt.Println("fan-daemon started", "sim=", *sim)
	if err := d.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}