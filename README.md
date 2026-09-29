# infinianalytics-agent

The InfiniAnalytics server agent: a small program installed on a client's server that sends
its CPU, memory, disks, network and Docker containers to InfiniAnalytics every 10 seconds, and
says **why** the machine went away when it does - reboot, shutdown, crash, agent stopped or
no connection. The dashboard shows it under **Infraestructura → Servidores**.

One static binary per platform (Linux and Windows, amd64 and arm64), no runtime dependencies,
~10 MB of memory, capped by its service at 20 % of one core.

## Install

Use the dashboard: **Servidores → Añadir servidor** asks for a name and a department and
prints the command with a one-time code (valid 1 hour):

```sh
# Linux (systemd)
curl -fsSL https://github.com/InfiniWorkspace/infinianalytics-agent/releases/latest/download/install.sh \
  | sudo sh -s -- --code XXXX-XXXX-XXXX --url https://api.analytics.infini.es
```

```powershell
# Windows - elevated PowerShell
& ([scriptblock]::Create((irm 'https://github.com/InfiniWorkspace/infinianalytics-agent/releases/latest/download/install.ps1'))) -Code 'XXXX-XXXX-XXXX' -Url 'https://api.analytics.infini.es'
```

```sh
# Docker - the container reports the HOST (host network, / read-only, Docker socket)
docker run -d --name infinianalytics-agent --restart unless-stopped --network host   -v /var/run/docker.sock:/var/run/docker.sock:ro -v /:/hostfs:ro -v infinianalytics-agent:/state   -e IA_AGENT_ENROLL_CODE=XXXX-XXXX-XXXX -e IA_AGENT_URL=https://api.analytics.infini.es   ghcr.io/infiniworkspace/infinianalytics-agent:latest
```

The code is only used on the first start; the key it is traded for lives in the `/state`
volume. In the container a host reboot is seen as a plain `docker stop`, so it is reported as
"motivo desconocido" rather than "reinicio".

The dialog turns to **Conectado** as soon as the first batch arrives. Running either script
again without a code upgrades the binary and restarts the service.

By hand:

```sh
infinianalytics-agent enroll XXXX-XXXX-XXXX --url https://api.analytics.infini.es
sudo infinianalytics-agent install      # systemd unit / Windows service, enabled and started
infinianalytics-agent status            # last push, pending spool
```

`enroll` trades the code for this server's own key and saves it in `agent.env`
(`/var/lib/infinianalytics-agent/` on Linux, `%ProgramData%\InfiniAnalytics Agent\` on
Windows), readable only by root / SYSTEM and Administrators. Re-enrolling the same machine
(same `/etc/machine-id` or `MachineGuid`) re-binds to the same server and keeps its history.
All settings are in [`agent.env.example`](agent.env.example).

## How it works

- **Sampling.** Vitals every 2 s, folded into a 10 s window: min / mean / max of CPU and
  memory, mean / max of swap, network and disk throughput, the busiest single core, load and
  temperature. The per-core list is only sent as the current reading. Filesystems every 60 s.
- **Containers.** Once per window, the busiest 50 containers by CPU + memory (stopped ones
  fill any room left), keyed by compose `project/service` so a redeploy continues the same
  series. Docker's event stream adds starts, stops, crashes with exit codes, OOM kills,
  restarts, health changes and deploys (a start on a new image).
- **Spool.** Every closed window is written to an append-only, fsynced spool before it is
  pushed, and removed once the backend acknowledges it. While the backend is unreachable it
  keeps up to 48 h / 50 MB (oldest dropped first) and drains oldest-first, an hour per request,
  when it is back - the charts have no hole.
- **Pushing.** One gzip POST every 10 s (the backend can ask for another cadence), with
  exponential backoff and jitter on 5xx / network errors. `401` / `410` (key replaced, server
  deleted from the dashboard) stop pushing until `enroll` is run again.
- **Why it stopped.** On SIGTERM the agent asks systemd what is queued (`reboot.target` →
  reboot, `poweroff.target` → shutdown, nothing → the service was stopped); the Windows service
  accepts PRESHUTDOWN for the same purpose. The `stopping` event is spooled first, then pushed
  with a 3 s timeout. Together with the kernel boot id this is how the backend tells a reboot
  from a crash from a network cut.

The wire format is contract v1, specified as JSON Schema in the backend repository
(`infinianalytics-back-fastapi/docs/wire-v1.json`).

## Development

```sh
go test ./...          # unit tests
go vet ./... && GOOS=windows go vet ./...
scripts/smoke.sh       # the real binary against a fake backend: pushes, a 503 outage, spool drain
```

### Fork of kanshi

This repository is a fork of [kanshi](https://github.com/FernandoInfini/kanshi) that reuses
its collectors. The rules that keep merging upstream painless:

- `internal/vitals`, `internal/dockerstats` and `internal/roots` are **never edited here** - a
  fix goes upstream first, then `git fetch upstream && git merge upstream/main`.
- The module path stays `github.com/rene-roid/kanshi`, because those packages import each
  other by it; renaming it would mean editing them.
- `internal/agent/dockerdial*.go` repeats kanshi's unexported Docker dialer for the event
  stream. Once upstream exports it (`dockerstats.Dialer`), delete the copy.
- Removed from the fork: the web dashboard, the storage map, the access modes and the Docker
  image. `internal/config` is fork-owned (`IA_AGENT_*`, `agent.env`).

### License

Inherited from kanshi: AGPL-3.0 (see [LICENSE](LICENSE)). **Decide before shipping** - the
author of both is free to relicense the fork.
