# fan_optimizer

CPU-light Go daemon: empirical case/CPU fan groups + human GPU curve.

## Build

```bash
CGO_ENABLED=0 go build -o fan-daemon ./cmd/fan-daemon
CGO_ENABLED=0 go build -o fanctl ./cmd/fanctl
```

Cross-compile for Linux:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o fan-daemon ./cmd/fan-daemon
```

## Sim (no hardware)

```bash
./fan-daemon -sim -groups config/groups.example.json
```

## Config

- `config/groups.example.json` — PWM groups, TDP, targets
- `config/gpu_curve.json` — GPU `t_on`, slew, scale
- `config/human.json` — noise weight, search aggression

Profile knots: `data/profile.json`

## Auto-detect fan groups

**Daisy-chained / splitter fans** share one motherboard PWM header → the kernel exposes **one** `pwmN` file. Discovery creates **one optimizer group** for that header (all chained fans move together).

Discovery does **not** know “front vs rear” — only physical PWM outputs. On the Linux PC:

```bash
sudo fanctl discover config/groups.json
```

Review the list, merge two headers into one group in JSON only if BIOS syncs them separately but they should move together (put multiple entries in `channels`).

## GPU curve web UI

Stdlib HTTP only, bind localhost by default.

**With daemon** (applies immediately):

```bash
./fan-daemon -groups config/groups.example.json -web 127.0.0.1:8765
```

Open http://127.0.0.1:8765 — sliders for `t_on`, `t_off`, min duty, `t_full`, scale, ramp slew. Preview curve chart. **Save & apply** writes `gpu_curve.json` and reloads the daemon.

**Standalone** (daemon must already be running for apply via socket):

```bash
./fan-web -addr 127.0.0.1:8765 -gpu config/gpu_curve.json
```

Do not expose the UI on `0.0.0.0` without a reverse proxy and auth.

## fanctl

```bash
fanctl too-loud
fanctl too-hot
fanctl observe
fanctl gpu set --config config/gpu_curve.json
```

## systemd

See `deploy/fan-daemon.service`