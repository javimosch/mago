---
name: fleet
description: run workers in production — many workers, autonomy knobs, keepalive + scheduled stop
---

# Worker fleet & scheduling

mago handles **timed stop** and **staggering** natively (flags below). It does NOT supervise
processes — restart-on-crash and start-on-boot are still the OS's job (cron/systemd), but that's now
one line, not a script.

## Native lifecycle (`mago serve`)
- `--daemon` — detach a **supervisor** (per-company pidfile + `.mago/worker.log`) that keeps the worker
  up: **restarts it on crash** (with backoff), but exits when the worker stops cleanly. Replaces nohup
  + the keepalive cron.
- `mago serve stop -C <dir>` / `mago serve status -C <dir>` — control/inspect a daemonized worker.
- `--until HH:MM` — stop cleanly at the next occurrence of that 24h time (no cron kill script needed).
- `--start-delay <dur>` — wait before serving, e.g. `15m` / `900s` (stagger workers; no `sleep` wrapper).
- `--heartbeat <secs>` — fallback reconcile cadence if a webhook is missed.

A supervised, staggered worker that runs until 9am and **stays stopped** (the supervisor exits when
the worker hits `--until`) is one command:
```
mago serve --relay --daemon --start-delay 15m --until 09:00 -C /root/co
mago serve status -C /root/co     # running (supervisor pid …)
mago serve stop   -C /root/co     # stop early
```

## Multiple workers per account
Run a worker on as many machines as you like under one account/license, each with its own company +
repos.
- Each worker sends an id: `MAGO_WORKER_ID` (defaults to hostname). Distinct ids coexist on the relay.
- The platform routes each repo's events to **exactly one** worker. Give each worker **different**
  repos to parallelize — two workers on the same repo means only one receives its events.
- Use the same account on each machine: copy `~/.mago/config.json` (holds the license), or `mago login`.

## Runtime mode (switch a worker WITHOUT restarting it)
A worker's mode — proactive cadence, non-code comms, and merge policy — is read **live every cycle**
from `.mago/mode.json`, so you can re-aim a running worker with no restart, no ssh, no env edit.

- **Local** (same machine as the worker): `mago mode <tokens> -C <dir>` — writes the file; a running
  worker picks it up within ~30s. `mago mode show -C <dir>` prints the current mode.
- **Remote** (over the relay, to a connected `--relay` worker): `mago worker mode <tokens> --worker <id>`
  (its `MAGO_WORKER_ID` / hostname) or `--all` for every worker on your account. The platform pushes a
  control frame down the worker's existing relay connection; it applies + persists instantly.

Tokens (presets + `key=value`, combine freely):
- `reactive` (proactive off) · `proactive` (default 1h) · `proactive=<secs>` · `comms=on|off`
- `review` (you merge) · `verified` (auto-merge only green PRs) · `merge=review|verified|on`

```
mago mode proactive=3600 verified comms=on -C /root/co   # local, live
mago worker mode reactive --worker rbm21                  # remote: stop proposing, just react
mago worker mode verified --all                           # remote: flip the whole fleet to verified autonomy
```
When `.mago/mode.json` is absent the mode is derived from the env knobs below (back-compat); once you
set a mode, the file wins.

## Autonomy knobs (per worker, via env — seed the default mode)
- `MAGO_PROACTIVE=<secs>` — the planner proposes new issues from STATE.md `## Mission` on this cadence.
- `MAGO_PROACTIVE_MAX=<n>` — max proposals per cycle (default 2; set 1 for a gentle drip).
- `MAGO_COMMS=1` — the CMO posts a release note when a `mago/task-*` PR merges.
- `MAGO_NO_MERGE=1` — reviewer comments but never merges (you merge). Safe default on real repos.
- `MAGO_VERIFY=1` or `MAGO_VERIFY_CMD="<cmd>"` — reviewer checks out the PR + runs build/tests
  (auto-detects Go: `go build ./... && go test ./...`). Auto-merge then needs approve **AND** green.
  Drop `MAGO_NO_MERGE` + set this for **verified autonomy** (only green PRs land).
- `MAGO_MERGE_UNVERIFIED=1` — allow auto-merge when a repo has no detectable check.
- `MAGO_DAILY_BUDGET=<n>` — cap autonomous work cycles per UTC day (worker pauses when hit; resets at
  UTC midnight). A "cycle" ≈ a reconcile / review / release-note / planning round.
- Claude Code as **root**: `IS_SANDBOX=1` is required for tool use (mago sets it automatically when euid==0).

## Persistent across reboots
`--daemon` supervises (crash-restart) and `--until` handles the timed stop, so the OS only needs to
**start it on boot**. A launcher + one `@reboot` line:
```
# /root/co/run.sh
#!/usr/bin/env bash
export MAGO_PLATFORM_URL=https://mago.intrane.fr MAGO_GH_REPO=owner/repo MAGO_TASK_LABEL=mago
export MAGO_PROVIDER=claude MAGO_MODEL=sonnet
export MAGO_PROACTIVE=3600 MAGO_NO_MERGE=1 MAGO_DAILY_BUDGET=20
exec mago serve --relay --daemon --start-delay 15m --until 09:00 -C /root/co

# crontab -e   (boot only — no keepalive/stop scripts needed)
@reboot /root/co/run.sh >> /root/co/boot.log 2>&1
```
No keepalive loop, no stop script, no `sleep` wrapper — the daemon supervises and self-stops.
(Prefer systemd? Run the **foreground** `mago serve --relay --until 09:00 …` as a unit with
`Restart=on-failure`; skip `--daemon` and let systemd supervise.)

## Check in
- Per company: `mago digest -C <company>` — backlog, PRs (24h), **shipped by mago**, HITL, budget usage.
- Across all your workers (platform side, operator): `mago-platform activity`.
