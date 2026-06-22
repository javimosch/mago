# CLI usage

Two binaries. `mago` = the client/worker (open-source core). `mago-platform` = operator-private
control plane. Run `mago help` for the live list.

## `mago` (client + worker)

Account (thin HTTP client to the platform; token+license cached in `~/.mago/config.json`, 0600):
```
mago register [--email <e>] [--password <p>]   # → token; creds also from $MAGO_PASSWORD or prompt
mago login    [--email <e>] [--password <p>]
mago subscribe                                 # prints the Stripe checkout link (€20/mo)
mago account status                            # plan + license; caches license_key for the worker
mago link --installation <id>                  # claim a GitHub App installation (entitles your repos)
mago link list                                 # linked installations + entitled repos
```

Company / worker:
```
mago init [dir]                                # scaffold .mago/, STATE.md, tasks/, workspace/, exec team
mago task add "<title>" [--project <p>] [-C d] # add a task (GitHub issue when MAGO_GH_REPO set)
mago project add <name> --repo owner/repo [--mirror] [-C d]  # register a project (--mirror = open+close a tracking issue on the project repo per task)
mago project list [-C d]
mago serve [-C d] [--relay] [--heartbeat <s>] [--addr :8099] [--secret <hmac>]   # the worker
mago run <agent> [-C d]                        # one tick   ·   mago tick [-C d] = reconcile once
mago loop <agent> [-C d]                       # adaptive-cadence ticks
mago status [-C d]                             # STATE.md, projects, tasks, pending HITL
mago digest [-C d]                             # "what your company did": backlog, PRs (24h), HITL, budget
mago answer <task-id> "<text>" [-C d]          # answer a needs_human task
```

Autonomy (let a company run unattended): `MAGO_PROACTIVE=<secs>` ticks the planner to file
mission-driven issues itself; `MAGO_COMMS=1` makes the CMO post a release note when a `mago/task-*`
PR merges; `MAGO_DAILY_BUDGET=<n>` caps autonomous work cycles per UTC day (a reconcile/review/
release-note/planning round each = 1; 0 = unlimited) and the worker pauses + says so when hit.
`mago digest` shows the day's activity + budget usage; cron it or run it to check in.

Backlog scoping (opt-in): set `MAGO_TASK_LABEL=mago` so the worker only reconciles issues
carrying that label — lets `MAGO_GH_REPO` point at a real project repo without touching its other
issues; adding the label to an existing issue triggers pickup (the `issues.labeled` wake fires
only for this human-applied label, never mago's own labels).

Env: `MAGO_COMPANY` (default `-C`), `MAGO_GH_REPO` (GitHub-backed company), `MAGO_PLATFORM_URL`
(default `http://localhost:9100`), `MAGO_PASSWORD`, `MAGO_PROVIDER`/`MAGO_MODEL` (override the
agents' tau provider/model — use `opencode-go`/`deepseek-v4-flash` for the working provider).
**BYOK provider key:** put it in `~/.config/tau/config.json` (`{"keys": {"opencode-go": "sk-..."}}`,
chmod 600) — used automatically (tau#30). Or export `OPENCODE_API_KEY` (`DEEPSEEK_API_KEY`/
`OPENAI_API_KEY`). Without either, tau uses a rate-limited keyless path; `mago serve` warns.

## `mago-platform` (operator)

```
mago-platform start [--port N] [--daemon]   # run the server (hotify cmd / daemon); --daemon detaches
mago-platform stop | status                 # pidfile lifecycle (~/.mago-platform/)
mago-platform setup-github --url https://<host> [--org <org>]   # create the GitHub App (1 click)
mago-platform webhook add  --repo owner/repo --url https://<host> [--account <email>]   # operator-token hook
mago-platform webhook list|rm|ping --repo owner/repo [--id N]
```

Config: `platform/.env` (gitignored) or `.env` beside the binary, or `$MAGO_PLATFORM_ENV`. Keys:
`APP_URL`, `PORT`, `JWT_SECRET`, `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_PRICE_MAGO`,
`GITHUB_WEBHOOK_SECRET`, `GITHUB_APP_ID` (+ slug/key/client from setup-github),
`GITHUB_ENFORCE_ENTITLEMENT`, `GITHUB_TOKEN_FILE` (default `~/.github/token`), `DB_PATH`.
