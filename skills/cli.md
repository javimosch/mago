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
mago task add "<title>" [--project <p>] [-C d]  add a task (GitHub issue when MAGO_GH_REPO set)
mago project add <name> --repo owner/repo [-C d]
mago serve [-C d] [--relay] [--heartbeat <s>] [--until HH:MM] [--start-delay <dur>]  the worker
mago tick [-C d]                                reconcile once (route + run agents)
mago status [-C d]                              STATE.md, tasks, pending HITL
mago digest [-C d]                              "what your company did": backlog, PRs (24h), HITL, budget
mago answer <task-id> "<text>" [-C d]           answer a needs_human task
mago skills [<name>]                            these embedded operator skills (version-matched to the binary)
```

## Environment
- `MAGO_PLATFORM_URL` (default https://mago.intrane.fr), `MAGO_COMPANY` (default `-C`), `MAGO_PASSWORD`.
- `MAGO_GH_REPO` — GitHub-backed company repo; `MAGO_TASK_LABEL=mago` — only act on labeled issues.
- Harness: `MAGO_PROVIDER` / `MAGO_MODEL` (`claude`/`sonnet`|`opus`, or `opencode-go`/`deepseek-v4-flash`);
  `OPENCODE_API_KEY` for tau; `CLAUDE_CONFIG_DIR` for Claude Code under a custom HOME.
- Autonomy (see the `fleet` skill): `MAGO_PROACTIVE`, `MAGO_PROACTIVE_MAX`, `MAGO_COMMS`, `MAGO_NO_MERGE`,
  `MAGO_VERIFY` / `MAGO_VERIFY_CMD`, `MAGO_MERGE_UNVERIFIED`, `MAGO_DAILY_BUDGET`, `MAGO_WORKER_ID`.

## Exit codes
`0` ok · `1` error · `80` usage/user error · `100-109` integration error (missing/unauth dependency).
