package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexpy/fan_optimizer/internal/config"
	"github.com/alexpy/fan_optimizer/internal/webui"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "listen address (localhost only recommended)")
	gpuPath := flag.String("gpu", "config/gpu_curve.json", "GPU curve JSON path")
	groupsPath := flag.String("groups", "config/groups.example.json", "for socket path")
	flag.Parse()

	var groups config.GroupsConfig
	_ = config.LoadJSON(*groupsPath, &groups)
	groups.Defaults()
	socket := groups.SocketPath
	if socket == "" {
		socket = "data/fan-daemon.sock"
	}

	srv := &webui.Server{
		Addr:       *addr,
		GPUPath:    *gpuPath,
		SocketPath: socket,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	fmt.Println("GPU curve UI:", "http://"+*addr)
	if err := srv.Run(ctx); err != nil && err != context.Canceled {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}