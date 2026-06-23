---
name: fleet
description: run workers in production — many workers, autonomy knobs, keepalive + scheduled stop
---

# Worker fleet & scheduling

**mago has NO built-in scheduler or process manager.** A worker (`mago serve --relay`) runs until
killed; its only timing is the proactive cadence and `--heartbeat`. Fleet lifecycle — keepalive,
staggering, "run until 9am" — is the operator's job via the OS (cron/systemd). This skill is the
pattern; mago provides the worker, you provide the orchestration.

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

## Run a worker as a persistent service (keepalive + reboot)
A launcher script plus a cron keepalive that restarts it within minutes if it dies and after reboot:
```
# /root/co/run.sh
#!/usr/bin/env bash
export MAGO_PLATFORM_URL=https://mago.intrane.fr MAGO_GH_REPO=owner/repo MAGO_TASK_LABEL=mago
export MAGO_PROVIDER=claude MAGO_MODEL=sonnet
export MAGO_PROACTIVE=3600 MAGO_NO_MERGE=1 MAGO_DAILY_BUDGET=20
exec mago serve --relay -C /root/co

# crontab -e
*/3 * * * * pgrep -f "serve --relay -C /root/co" >/dev/null || /root/co/run.sh >> /root/co/worker.log 2>&1
@reboot /root/co/run.sh >> /root/co/worker.log 2>&1
```

## Schedule a stop ("run until 9am")
mago has no timed stop — use cron. A self-removing 09:00 stop:
```
# /root/stop.sh
#!/usr/bin/env bash
crontab -l 2>/dev/null | grep -vE "co/run.sh|stop.sh" | crontab -    # remove keepalive + this stop
pkill -f "mago serve --relay"

# crontab -e
0 9 * * * /root/stop.sh >> /root/stop.log 2>&1
```

## Stagger multiple workers
Add an initial `sleep <offset>` before `exec mago serve …` in each run.sh (e.g. 0, 900, 1800s) so the
workers' cadences are phase-offset and don't all fire at once. (Re-applied on each keepalive restart.)

## Check in
- Per company: `mago digest -C <company>` — backlog, PRs (24h), **shipped by mago**, HITL, budget usage.
- Across all your workers (platform side, operator): `mago-platform activity`.
