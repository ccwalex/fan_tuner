# fan_optimizer — feature specification

This document describes what the project is designed to do, how the major pieces fit together, and what is intentionally *not* handled. For build, flags, and day-to-day commands, see [README.md](README.md).

## Purpose

A **CPU-light** background daemon for Linux desktops with **one CPU** and **up to two NVIDIA GPUs**. It adjusts **case and CPU cooler fans** from sensor data using **empirical, black-box optimization**—no model of case airflow, intake vs exhaust roles, or fan placement. **GPU fans** stay on a **human-defined temperature curve** (start/stop, scaling, ramp limits); they are never modified by the case-fan search.

Design goals:

- **Portable**: Go, standard library only (`CGO_ENABLED=0`), JSON configs, no Python runtime or heavy ML stack.
- **Unobtrusive**: Default ~0.5 Hz control loop, `GOMAXPROCS(1)`, suitable for compilation and ML workloads when niced (see `deploy/fan-daemon.service`).
- **Self-adjusting**: Continuous load and ambient context; re-search when targets are missed or context drifts—not a one-time static grid calibration.

## Actuators and sensing

| Source | Data | Use |
|--------|------|-----|
| `/sys/class/hwmon` | CPU/package temp, optional ambient proxy, PWM duty, fan RPM | Case/CPU groups; manual PWM via `pwm*_enable=1` |
| Intel RAPL | `energy_uj` deltas | Normalized CPU power context |
| `nvidia-smi` (CSV, cached ~2 s) | Power, GPU temp, fan speed | GPU context, curve input, safety caps |

On shutdown, the daemon (and systemd `ExecStopPost`) can restore **automatic** PWM mode so BIOS/firmware curves resume.

**Simulation** (`-sim`): a lumped thermal plant with randomized hidden coupling between fan groups and heat sources, so development and tuning can run on non-Linux hosts without hardware.

## Dual-controller architecture (single daemon)

```
┌─────────────────────────────────────────────────────────────┐
│                      fan-daemon (0.5 Hz)                    │
├────────────────────────────┬────────────────────────────────┤
│ Case / CPU fan groups      │ NVIDIA GPU fans (0..2)         │
│ · IDW from profile knots   │ · Human curve only (t_on,      │
│ · Differential search      │   t_off, min_duty, t_full,     │
│ · Commit ≥ commit_pwm_*    │   scale, slew_up/down)         │
│ · Never touches GPU curve  │ · t_crit → 100% (safety)       │
└────────────────────────────┴────────────────────────────────┘
```

Case groups are **logical PWM bundles** defined in config (or produced by discovery). The optimizer does not need labels like “front intake”; it only sees group indices and duties.

## Load and ambient context

**Power context** `x` is normalized to TDP fractions: `(P_cpu/TDP_cpu, P_gpu0/TDP_gpu0, P_gpu1/TDP_gpu1)`, smoothed with an EMA in the daemon. **Ambient** `T_amb` comes from a configured sensor or an idle-chip proxy.

**Knots** in the profile store:

- Position: `(x, T_amb)` (continuous; grid steps are discretization hints, not isolated offline cells).
- Duties: current best `u`, BIOS seed snapshot `u0`, and generation metadata.

**Interpolation**: inverse distance weighting (IDW) over known knots inside the hull of observed points. Output duties are slew-limited before actuation.

## Lifecycle: observe → control → learn

1. **Observe** (initial or `fanctl observe`): Read whatever PWM the firmware/BIOS is driving; record as `u0` and the first knot for the current `(x, T_amb)`.
2. **Control**: Take manual PWM; begin from `u0` and interpolated neighbors.
3. **Stability gate**: Search and knot commits run only after load has been stable for `stable_sec` (configurable).

There is **no** separate “season” variable. HVAC, weather, desk placement, or room changes appear as **measured `T_amb` drift** (`ambient_step_c`) or repeated target misses, which trigger re-search like any other context change.

## Online search and commit policy

Search runs when:

- Steady-state **temperature exceeds targets** while `x` remains in the same effective cell, or
- **Context changes** (new load/ambient step), requiring interpolation from prior knots plus exploration.

**Trials** perturb **relative** duties across groups (differential splits)—e.g. biasing CPU cooler flow without assuming it only cools the CPU. Scoring uses overshoot vs human targets (with optional offset from feedback), ambient compensation, and a noise penalty weight.

**Commit gate**: A trial may apply live immediately for transient correction, but the profile knot is **updated only** if the candidate differs from the cell reference `u_ref` by at least **`commit_pwm_threshold`** (default **10%** PWM) on at least one group.

After interpolation, **random differential searches** still run from the interpolated point so cross-component effects (e.g. CPU fan helping GPU temps) can be discovered empirically.

**Hard caps**: If any monitored temp crosses `temp_hard_cap_c`, duties clamp toward maximum regardless of profile (fail-safe).

## GPU fan curve (human only)

Per-GPU parameters (JSON + web UI):

| Parameter | Role |
|-----------|------|
| `t_on` / `t_off` | Hysteresis so fans do not chatter around the threshold |
| `min_duty` | Floor when the curve is active |
| `t_full`, `scale` | Map temperature to target duty |
| `slew_up` / `slew_down` | Max %/s change (acoustic smoothing) |
| `t_crit` | Emergency full speed |

Mode `nvidia-auto` defers to the driver where configured; `curve` drives fan duty through the backend. Bayesian / profile search **never** writes GPU curve fields.

**Web UI** (`-web` on daemon or `fan-web`): localhost sliders and canvas preview; save writes `gpu_curve.json` and signals reload (`reload-gpu` over the Unix socket).

## Human feedback

Unix socket commands (also via `fanctl`):

| Command | Effect |
|---------|--------|
| `too-loud` | Increases noise penalty; optimizer favors quieter duty splits |
| `too-hot` | Increases search aggressiveness; tightens effective temp targets |
| `observe` | Return to BIOS snapshot seeding for current context |
| `seed-done` | Mark manual seed workflow complete |
| `reload-gpu` | Reload GPU curve from disk |
| `sim-load` | (simulation) Inject synthetic load for testing |

Preferences live in `config/human.json` and are merged with ephemeral flags in the feedback store.

## Auto-detection of fan groups

`fanctl discover` scans hwmon for `pwm*` entries. **One sysfs PWM channel = one optimizer group.** Daisy-chained or splitter fans on a single header appear as a single group because they share one electrical PWM signal. Discovery does not infer chassis geometry; names are for humans editing JSON.

Multiple physical channels that should move together can be merged manually by listing several `channels` under one group `name`.

## Artifacts and IPC

| Path / channel | Content |
|----------------|---------|
| `data/profile.json` | Knots, mode (`observe` / `control`), generations |
| `data/observations.jsonl` | Append-only trial and telemetry log |
| Unix domain socket (`socket_path` in groups config) | JSON-line commands |
| `config/groups*.json` | Groups, TDP, targets, timing, paths |
| `config/gpu_curve.json` | GPU curve |
| `config/human.json` | Noise and search knobs |

## Binaries

| Binary | Role |
|--------|------|
| `fan-daemon` | Main loop, sensors, profile, optional embedded web UI (`-web`) |
| `fanctl` | CLI over socket; discover, feedback, GPU config, sim helpers |
| `fan-web` | Standalone GPU curve UI (daemon must run for live apply) |

## Explicit non-goals

- No Python backend or external Go modules in the core tree.
- No hardcoded case topology or “intake ratio” templates.
- No Bayesian optimization of GPU fan curves.
- No macOS production path (`powermetrics`); macOS is for **simulation and UI dev** only.
- No remote exposure of the web UI without your own auth and reverse proxy (defaults bind localhost).

## Summary

**Case/CPU**: Measure power and temp → interpolate duties from learned knots → explore differential splits when targets fail or context moves → persist improvements only when they matter (≥10% PWM). **GPU**: Human curve with hysteresis and slew limits, hot-reloadable from the web UI. **Environment**: Ambient and placement changes flow through real sensors, not seasonal presets. **Ops**: Small static binary, JSON configs, discovery for PWM headers, systemd unit with low scheduling priority.