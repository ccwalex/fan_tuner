package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/alexpy/fan_optimizer/internal/config"
	"github.com/alexpy/fan_optimizer/internal/discover"
)

func main() {
	socket := flag.String("socket", "data/fan-daemon.sock", "daemon unix socket")
	groupsPath := flag.String("groups", "config/groups.example.json", "for data_dir default")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	var groups config.GroupsConfig
	_ = config.LoadJSON(*groupsPath, &groups)
	groups.Defaults()
	if *socket == "/run/fan-daemon.sock" && groups.SocketPath != "" {
		*socket = groups.SocketPath
	}

	cmd := args[0]
	switch cmd {
	case "too-loud", "too-hot", "observe", "seed-done":
		send(*socket, map[string]string{"cmd": cmd})
	case "gpu":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: fanctl gpu set --config path")
			os.Exit(1)
		}
		if args[1] == "set" && len(args) >= 3 && args[2] == "--config" && len(args) >= 4 {
			send(*socket, map[string]string{"cmd": "reload-gpu", "path": args[3]})
		} else {
			fmt.Fprintln(os.Stderr, "usage: fanctl gpu set --config config/gpu_curve.json")
			os.Exit(1)
		}
	case "discover":
		if runtime.GOOS != "linux" {
			fmt.Fprintln(os.Stderr, "discover requires Linux sysfs (/sys/class/hwmon)")
			os.Exit(1)
		}
		outPath := "config/groups.discovered.json"
		if len(args) >= 2 {
			outPath = args[1]
		}
		headers, err := discover.ScanPWM()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if len(headers) == 0 {
			fmt.Fprintln(os.Stderr, "no pwm controls found; check pwmconfig / permissions")
			os.Exit(1)
		}
		cfg := discover.DefaultGroupsConfig(headers)
		// preserve targets from example if merging manually later
		cfg.TempTargetsC = config.TempTargets{CPU: 75, GPU0: 78, GPU1: 78}
		cfg.TempHardCapC = config.TempTargets{CPU: 95, GPU0: 83, GPU1: 83}
		if err := config.SaveJSON(outPath, cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, h := range headers {
			fmt.Printf("%s %s pwm%d rpm=%d label=%q\n", h.Hwmon, h.HwmonName, h.PWM, h.FanRPM, h.Label)
		}
		fmt.Println("wrote", outPath, "—", len(cfg.Groups), "groups (one per PWM header; daisy-chain = one group)")
	case "sim-load":
		// sim-only helper via raw json extension
		if len(args) < 4 {
			fmt.Fprintln(os.Stderr, "usage: fanctl sim-load cpu gpu0 gpu1 (fractions)")
			return
		}
		send(*socket, map[string]string{
			"cmd": "sim-load", "cpu": args[1], "gpu0": args[2], "gpu1": args[3],
		})
	default:
		usage()
		os.Exit(1)
	}
}

func send(socket string, msg map[string]string) {
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer conn.Close()
	b, _ := json.Marshal(msg)
	_, _ = conn.Write(b)
	buf := make([]byte, 256)
	n, _ := conn.Read(buf)
	fmt.Println(string(buf[:n]))
}

func usage() {
	fmt.Println(`fanctl commands:
  discover [out.json]   — scan hwmon, one group per PWM (daisy-chained fans share one PWM)
  too-loud
  too-hot
  observe
  seed-done
  gpu set --config <path>`)
}