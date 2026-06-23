---
name: fleet
description: run workers in production — many workers, autonomy knobs, keepalive + scheduled stop
---

# Worker fleet & scheduling

mago handles **timed stop** and **staggering** natively (flags below). It does NOT supervise
processes — restart-on-crash and start-on-boot are still the OS's job (cron/systemd), but that's now
one line, not a script.

## Native lifecycle flags (`mago serve`)
- `--until HH:MM` — stop cleanly at the next occurrence of that 24h time (no cron kill script needed).
- `--start-delay <dur>` — wait before serving, e.g. `15m` / `900s` (stagger fleet workers; no `sleep` wrapper).
- `--heartbeat <secs>` — fallback reconcile cadence if a webhook is missed.

So a staggered, self-stopping worker is just:
```
mago serve --relay --start-delay 15m --until 09:00 -C /root/co
```

## Multiple workers per account
Run a worker on as many machines as you like under one account/license, each with its own company +
repos.
- Each worker sends an id: `MAGO_WORKER_ID` (defaults to hostname). Distinct ids coexist on the relay.
- The platform routes each repo's events to **exactly one** worker. Give each worker **different**
  repos to parallelize — two workers on the same repo means only one receives its events.
- Use the same account on each machine: copy `~/.mago/config.json` (holds the license), or `mago login`.

## Autonomy knobs (per worker, via env)
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

## Run a worker as a persistent service (restart-on-crash + boot)
mago does timed-stop + staggering itself; the OS only needs to handle crash-restart and boot. A
launcher + a cron keepalive (restarts within minutes if it dies; starts on reboot):
```
# /root/co/run.sh
#!/usr/bin/env bash
export MAGO_PLATFORM_URL=https://mago.intrane.fr MAGO_GH_REPO=owner/repo MAGO_TASK_LABEL=mago
export MAGO_PROVIDER=claude MAGO_MODEL=sonnet
export MAGO_PROACTIVE=3600 MAGO_NO_MERGE=1 MAGO_DAILY_BUDGET=20
exec mago serve --relay --start-delay 15m --until 09:00 -C /root/co   # stagger + stop are native now

# crontab -e
*/3 * * * * pgrep -f "serve --relay -C /root/co" >/dev/null || /root/co/run.sh >> /root/co/worker.log 2>&1
@reboot /root/co/run.sh >> /root/co/worker.log 2>&1
```
That's it — no separate stop script, no `sleep` wrapper. The worker stops itself at 09:00; the `*/3`
line only restarts it if it actually crashed (a real supervisor like systemd works too). NOTE: with a
keepalive, the worker will be relaunched after its `--until` stop — drop the `*/3` line (keep just
`@reboot`) if you want it to stay stopped after 09:00.

## Check in
- Per company: `mago digest -C <company>` — backlog, PRs (24h), **shipped by mago**, HITL, budget usage.
- Across all your workers (platform side, operator): `mago-platform activity`.
