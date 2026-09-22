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
- `pr-cap=<n>` · `issue-cap=<n>` — per-repo backpressure / cadence control (0 = off; see below)
- `update=auto|manual` — self-update policy (see "Worker self-update" below)

```
mago mode proactive=3600 verified comms=on -C /root/co   # local, live
mago worker mode reactive --worker rbm21                  # remote: stop proposing, just react
mago worker mode verified --all                           # remote: flip the whole fleet to verified autonomy
```
When `.mago/mode.json` is absent the mode is derived from the env knobs below (back-compat); once you
set a mode, the file wins.

## Worker self-update (no reship, no ssh)
A relay worker learns the latest CLI version (a content hash) from the platform on its existing
connection and can update itself (cli-update-spec — see `docs/SELF-UPDATE.md`):
- `update=manual` (**default**) — the worker logs a one-time nudge when a newer binary is
  published; you update it on demand with `mago update` (or `--check` first: exits 5 when an
  update is available). The nudge never updates anything by itself.
- `update=auto` — on a new release the worker downloads `/dl/mago` for its os/arch, verifies the
  hash, smoke-tests the download, atomically swaps its own binary (the old one is kept at
  `<exe>.bak` — roll back by hand with `mv mago.bak mago`), and re-execs in place (same PID — a
  `--daemon` supervisor neither double-spawns nor needs restarting). Flip it live:
  `mago worker mode update=auto --all`.

**Install location matters:** the swap stages a temp file beside the binary, so the binary's
directory must be writable by the user the worker runs as. Install to the worker user's own
`~/.local/bin` (`mago install` as that user) — NOT a root-owned shared prefix like
`/usr/local/bin`, where self-update can never write. If a worker is already under an unwritable
prefix, `update=auto` reports the permission error once (path + user) and falls back to nudging;
relocate it with `mago install && ~/.local/bin/mago update` and restart from the new path.

So shipping a release to the whole fleet is: rebuild the matrix → scp the new `mago-<os>-<arch>` into
the platform's `cli/` dir → auto workers pick it up within ~25s; manual workers nudge until updated.
No per-box ssh. (Env seed: `MAGO_UPDATE=auto`.) The version is a sha256 of the binary, so there's no
version-bump discipline — identical bytes never trigger an update.

## Autonomy knobs (per worker, via env — seed the default mode)
- `MAGO_PROACTIVE=<secs>` — the planner proposes new issues from STATE.md `## Mission` on this cadence.
  Proposals land in the **backlog repo only** — so proactive is single-repo; for a multi-project worker
  (one backlog + `project:` dispatch, see the `operating` skill) keep it reactive.
- `MAGO_PROACTIVE_MAX=<n>` — max proposals per cycle (default 2; set 1 for a gentle drip).
- `MAGO_COMMS=1` — the CMO posts a release note when a `mago/task-*` PR merges.
- `MAGO_NO_MERGE=1` — reviewer comments but never merges (you merge). Safe default on real repos.
- `MAGO_VERIFY=1` or `MAGO_VERIFY_CMD="<cmd>"` — reviewer checks out the PR + runs build/tests
  (auto-detects Go: `go build ./... && go test ./...`). Auto-merge then needs approve **AND** green.
  Drop `MAGO_NO_MERGE` + set this for **verified autonomy** (only green PRs land).
- `MAGO_MERGE_UNVERIFIED=1` — allow auto-merge when a repo has no detectable check.
- `MAGO_DAILY_BUDGET=<n>` — cap autonomous work cycles per UTC day (worker pauses when hit; resets at
  UTC midnight). A "cycle" ≈ a reconcile / review / release-note / planning round.
- `MAGO_PR_CAP=<n>` / `MAGO_ISSUE_CAP=<n>` — **per-repo cadence caps** (also `mago mode pr-cap=/issue-cap=`,
  live-switchable). `pr-cap`: once a repo has N **open mago PRs** (on `mago/` branches — unrelated
  human PRs don't count), the worker stops starting new implementation on it (open tasks wait,
  unassigned) until reviews/merges drain it back below N — so a repo on auto never piles past N open
  mago PRs. `issue-cap`: the proactive planner stops proposing
  once the repo has N open issues (overrides the built-in default of 3). Both `0` = off. The drains
  (review/merge) always run, so caps throttle *new* work without freezing the repo.
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
