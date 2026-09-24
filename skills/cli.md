---
name: cli
description: mago CLI command + environment-variable reference
---

# mago CLI reference

## Account (talks to the platform)
```
mago register [--email <e>] [--password <p>]   account + 48h trial; token -> ~/.mago/config.json (0600)
mago login [--email <e>] [--password <p>]
mago subscribe                                  €20/mo Stripe checkout link
mago billing                                    Stripe customer-portal link (manage/cancel)
mago account status                             plan, trial window, license
mago link --installation <id> | mago link list  claim/list GitHub App installs (entitle repos)
```

## Company / worker
```
mago init [dir]                                 scaffold .mago/, STATE.md, tasks/, workspace/, exec team
mago task add "<title>" [--project <p>] [-C d]  file an issue in the backlog repo; --project <p> tags
                                                project:<p> so its PR lands on that project's repo
mago project add <name> --repo owner/repo [--mirror] [-C d]   register a project repo
                                                (--mirror = open+close a tracking issue on the project repo per task)
mago serve [-C d] [--addr :8099] [--secret <hmac>] [--heartbeat <s>]
                  [--relay] [--daemon] [--until HH:MM] [--start-delay <dur>]  the worker
mago serve stop | status [-C d]                 control/inspect a --daemon worker
mago mode [show | <tokens>] [-C d]              switch a LOCAL worker's mode live (no restart)
mago worker mode <tokens> --worker <id>|--all   switch a REMOTE worker's mode over the relay
mago tick [-C d]                                reconcile once (route + run agents)
mago run <agent> [-C d]                         run ONE tick for one agent (brief -> harness -> write back)
mago loop <agent> [-C d] [--base/--max/--max-ticks <s>]  run ticks on an adaptive cadence
mago status [-C d]                              STATE.md, tasks, pending HITL
mago digest [-C d]                              "what your company did": backlog, PRs (24h), HITL, budget
mago answer <task-id> "<text>" [-C d]           answer a needs_human task
mago worker doctor                              validate gh auth + the configured LLM harness (exit 101 on failure)
mago skills [<name>]                            these embedded operator skills (version-matched to the binary)
mago feedback "<msg>" [--type bug|friction|feature|question]   report friction/bugs/requests to the mago team
mago update [--check] [--force]                 self-update this binary (check→download→verify→
                                                smoke→swap; old binary kept at <exe>.bak)
mago install [--prefix <dir>]                   copy this binary into <dir>/mago (default
                                                ~/.local/bin — the no-sudo spot self-update needs)
mago uninstall [--prefix <dir>]                 remove <prefix>/mago (no-op if absent)
```

## Environment
- `MAGO_PLATFORM_URL` (default http://localhost:9100), `MAGO_COMPANY` (default `-C`), `MAGO_PASSWORD`,
  `MAGO_WEBHOOK_SECRET` (webhook HMAC for `mago serve`; same as `--secret`).
- `MAGO_UPDATE=auto` — self-update the worker binary when the platform advertises a newer release
  (hash-verified, probe-run, atomic swap).
- `MAGO_GH_REPO` — the backlog/issue repo (single-repo: the one worked repo; multi-project: the command
  center where issues are filed). `MAGO_TASK_LABEL=mago` — only act on labeled issues. See `operating`.
- `MAGO_STATE_SYNC=1` — opt-in: publish the company's STATE.md + agent-defs into the repo (mago-state /
  default branch) for cross-machine sync. OFF by default so mago never writes its own files into a
  user's project repo when operating label-scoped. State always persists locally either way.
- Harness: `MAGO_PROVIDER` / `MAGO_MODEL` — **required, no default** (`claude`/`sonnet`|`opus`,
  `debri`/`SWE-1.6`, or a tau provider such as `opencode-go`/`deepseek-v4-flash`);
  `OPENCODE_API_KEY` for tau; `CLAUDE_CONFIG_DIR` for Claude Code under a custom HOME.
- Autonomy (see the `fleet` skill): `MAGO_PROACTIVE`, `MAGO_PROACTIVE_MAX`, `MAGO_COMMS`, `MAGO_NO_MERGE`,
  `MAGO_VERIFY` / `MAGO_VERIFY_CMD`, `MAGO_MERGE_UNVERIFIED`, `MAGO_DAILY_BUDGET`, `MAGO_PR_CAP`,
  `MAGO_ISSUE_CAP`, `MAGO_WORKER_ID`.
  These env vars seed the *default* mode; once set via `mago mode` / `mago worker mode` the persisted
  `.mago/mode.json` wins and is read live each cycle (see the `fleet` skill, "Runtime mode").

## Exit codes
`0` ok · `1` generic error · `80-89` user/usage · `90-99` resource (not found, already exists) ·
`100-109` integration (network, missing/unauth dependency) · `110-119` internal. Scripts should
branch on these ranges, not exact codes.
