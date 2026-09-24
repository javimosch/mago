# CLI usage

Two binaries. `mago` = the client/worker (open-source core). `mago-platform` = operator-private
control plane. Run `mago help` for the live list.

## `mago` (client + worker)

Account (thin HTTP client to the platform; token+license cached in `~/.mago/config.json`, 0600):
```
mago register [--email <e>] [--password <p>]   # → token; creds also from $MAGO_PASSWORD or prompt
mago login    [--email <e>] [--password <p>]
mago subscribe                                 # prints the Stripe checkout link (€20/mo)
mago billing                                   # Stripe customer-portal link (manage/cancel)
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
mago serve [-C d] [--relay] [--daemon] [--heartbeat <s>] [--addr :8099] [--secret <hmac>]
                  [--until HH:MM] [--start-delay <dur>]          # the worker
mago serve stop | status [-C d]                # control/inspect a --daemon worker
mago mode [show | <tokens>] [-C d]             # switch a LOCAL worker's mode live (no restart)
mago worker mode <tokens> --worker <id>|--all  # switch a REMOTE worker's mode over the relay
mago worker doctor                             # validate gh auth + LLM harness (exits 101 on failure)
mago run <agent> [-C d]                        # one tick   ·   mago tick [-C d] = reconcile once
mago loop <agent> [-C d]                       # adaptive-cadence ticks
mago status [-C d]                             # STATE.md, projects, tasks, pending HITL
mago digest [-C d]                             # "what your company did": backlog, PRs (24h), HITL, budget
mago answer <task-id> "<text>" [-C d]          # answer a needs_human task
mago skills [<name>]                           # embedded operator guides, version-matched to the binary
mago feedback "<msg>" [--type bug|friction|feature|question]   # report friction/bugs to the mago team
mago update [--check] [--force]                # self-update to the platform's release (.bak kept; --check exits 5)
mago install [--prefix <dir>]                  # copy binary to <dir>/mago (default ~/.local/bin — worker-writable)
mago uninstall [--prefix <dir>]                # remove <dir>/mago (no-op if absent)
```

Autonomy (let a company run unattended): `MAGO_PROACTIVE=<secs>` ticks the planner to file
mission-driven issues itself; `MAGO_COMMS=1` makes the CMO post a release note when a `mago/task-*`
PR merges; `MAGO_DAILY_BUDGET=<n>` caps autonomous work cycles per UTC day (a reconcile/review/
release-note/planning round each = 1; 0 = unlimited) and the worker pauses + says so when hit.
`mago digest` shows the day's activity + budget usage; cron it or run it to check in.

**Verified autonomy:** `MAGO_VERIFY=1` (or `MAGO_VERIFY_CMD="<cmd>"`) makes the reviewer check out
the PR + run build/tests (auto-detects Go) before approving; auto-merge then needs an approve **and**
a green check. Run with `MAGO_VERIFY=1` and `MAGO_NO_MERGE` off to let only verified-green PRs land
(`MAGO_MERGE_UNVERIFIED=1` to also merge when a repo has no detectable check).

**Multiple workers per account:** run a worker on as many machines as you like under the same
account/license — each with its own company + repos. They coexist on the relay (keyed by
`MAGO_WORKER_ID`, default the hostname), and the platform routes each repo's events to exactly one
worker. Run different companies/repos per machine to parallelize; distinct hostnames need no config.

Backlog scoping (opt-in): set `MAGO_TASK_LABEL=mago` so the worker only reconciles issues
carrying that label — lets `MAGO_GH_REPO` point at a real project repo without touching its other
issues; adding the label to an existing issue triggers pickup (the `issues.labeled` wake fires
only for this human-applied label, never mago's own labels).

Env: `MAGO_COMPANY` (default `-C`), `MAGO_GH_REPO` (GitHub-backed company), `MAGO_PLATFORM_URL`
(default `http://localhost:9100`), `MAGO_PASSWORD`, `MAGO_WEBHOOK_SECRET` (webhook HMAC for
`mago serve`; same as `--secret`), `MAGO_UPDATE=auto` (self-update the worker binary when the
platform advertises a newer release), `MAGO_PROVIDER`/`MAGO_MODEL` (override the
agents' provider/model — **required**, there is no default; **`claude`/`sonnet`** to run
agents on Claude Code (local subscription, no API key — see agent-runtime.md "Claude Code harness"),
or **`debri`/`SWE-1.6`** to run agents on devin via debri (local devin login, no API key — see
agent-runtime.md "Devin harness"; requires debri v1.1.0+ and tmux)).
**BYOK provider key:** put it in `~/.config/tau/config.json` (`{"keys": {"opencode-go": "sk-..."}}`,
chmod 600) — used automatically (tau#30). Or export `OPENCODE_API_KEY` (`DEEPSEEK_API_KEY`/
`OPENAI_API_KEY`). Without either, tau uses a rate-limited keyless path; `mago serve` warns.

## `mago-platform` (operator)

The platform server lives in the private `javimosch/mago-platform` repo and is not needed to
run anything in this one. The client side of it is `mago register/login/subscribe/account/link`
above, plus `mago serve --relay`.
