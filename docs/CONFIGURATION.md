# Configuration reference

This page documents every option that changes how mago runs. mago is configured
through three layers, in order of how often you'll touch them:

1. **Environment variables** (`MAGO_*` and a few provider keys) — the primary dial.
   mago reads them at process start; there is no config file to edit for these.
2. **JSON config files** — `~/.mago/config.json` (account/license) and
   `~/.config/tau/config.json` (provider API keys), both written by their respective
   `mago …` / `tau …` commands.
3. **CLI flags** — per-command overrides like `-C <dir>` and the `loop`/`serve` flags.

> **Note:** mago does **not** use a `mago.toml` (or any TOML) file. Configuration is
> environment-variable–first, with JSON for persisted account and provider state. If
> you've seen TOML referenced elsewhere, this page is the source of truth.

Most options are **opt-in**: with nothing set, `mago` runs a local company against the
current directory, unlimited work cycles, no GitHub, no auto-merge gating, and your
agent's default provider. Set only what you need.

---

## Quick start: the variables you'll actually set

```sh
# Pick the model your agents run on (BYOK — your provider bills you for tokens)
export MAGO_PROVIDER=opencode-go
export MAGO_MODEL=deepseek-v4-flash
export OPENCODE_API_KEY=...            # key for the provider above

# Drive a real GitHub repo: tasks become issues, HITL happens in comments
export MAGO_GH_REPO=owner/repo

# Keep spend predictable
export MAGO_DAILY_BUDGET=50            # max work cycles/day (0 = unlimited)
```

---

## Agent provider & model

| Variable | Default | What it does |
| --- | --- | --- |
| `MAGO_PROVIDER` | the agent's file-defined provider | Override the tau provider for **every** agent this run (e.g. `opencode-go`, `deepseek`, `openai`, `claude`). |
| `MAGO_MODEL` | the agent's file-defined model | Override the model for every agent (e.g. `deepseek-v4-flash`, `sonnet`). |

mago does not sell completions — you **bring your own key (BYOK)**. The key is read by
tau (the underlying agent runner), either from an environment variable or from
`~/.config/tau/config.json`. The env var depends on the provider:

| Provider (`MAGO_PROVIDER`) | API-key variable |
| --- | --- |
| `opencode-go` | `OPENCODE_API_KEY` |
| `deepseek` | `DEEPSEEK_API_KEY` |
| `openai` | `OPENAI_API_KEY` |
| `claude` | *(none — uses your local Claude Code subscription)* |

If no key is found for the resolved provider, mago prints a warning and tau falls back
to a rate-limited keyless path. As an alternative to the env var, add the key to
`~/.config/tau/config.json`:

```json
{ "keys": { "opencode-go": "sk-..." } }
```

(A global `"api_key": "..."` is also honored as a fallback for any provider.)

### Claude provider (subscription auth)

When `MAGO_PROVIDER=claude`, mago authenticates via your local Claude Code subscription
— no API key.

| Variable | Default | What it does |
| --- | --- | --- |
| `CLAUDE_CONFIG_DIR` | `~/.claude` | Point mago at your real Claude config when it runs under a custom `HOME`. |
| `IS_SANDBOX` | *(auto-set to `1` when running as root)* | Signals a sandboxed context so the agent can use its tools as root. Set explicitly to override the auto-detection. |

---

## Company & GitHub mode

| Variable | Default | What it does |
| --- | --- | --- |
| `MAGO_COMPANY` | current working directory | Default company directory when `-C <dir>` is not passed. |
| `MAGO_GH_REPO` | *(unset — local mode)* | `owner/repo` of the backlog repo. In GitHub mode, tasks become **issues**, PRs are opened against the repo, and human-in-the-loop happens in **comments**. Required when a company has multiple distinct project repos and the backlog repo is ambiguous. |
| `MAGO_GH_TOKEN` | *(unset — uses `gh` auth)* | Personal Access Token (repo scope) for GitHub API calls in GitHub-backed mode. When set, mago passes it to the `gh` CLI as `GH_TOKEN`; when unset, `gh` uses its own login (`gh auth login`). `mago worker doctor` verifies it is set in GitHub-backed mode. |
| `MAGO_TASK_LABEL` | *(unset)* | Scope the backlog to issues carrying this label. Lets `MAGO_GH_REPO` point at a real project repo while mago only acts on opted-in issues. |
| `MAGO_STATE_SYNC` | *(off)* | Set to `1` to push the company's state into `MAGO_GH_REPO`: runtime exhaust (STATE.md, `.mago/runs\|skills\|memory\|inbox`) to a `mago-state` branch, and agent definitions (`.mago/agents`, config, projects) to `main`. Off by default so mago doesn't pollute a user's project repo with its own branches — state always lives locally in the company dir regardless. Only meaningful when `MAGO_GH_REPO` is set. |

---

## Autonomy & budget

| Variable | Default | What it does |
| --- | --- | --- |
| `MAGO_DAILY_BUDGET` | `0` (unlimited) | Max work cycles per day. Usage persists in `.mago/usage.json` and resets daily. |
| `MAGO_PROACTIVE` | *(unset — off)* | Seconds between proactive-planning cycles. When set, the planner proposes new backlog from the company mission on this cadence. |
| `MAGO_PROACTIVE_MAX` | `2` | Max new issues filed per planning cycle. Set to `1` for a slower drip. |
| `MAGO_PR_CAP` | `0` (no cap) | Backpressure: stop starting new work on a repo once it has this many open `mago/task-*` PRs. Overridable live via `mago mode` / `.mago/mode.json` without a restart. |
| `MAGO_ISSUE_CAP` | `0` (falls back to a default of 3) | Backpressure: the planner stops proposing new backlog once the repo has this many open issues. Overridable live via `mago mode` / `.mago/mode.json` without a restart. |
| `MAGO_UPDATE` | *(manual)* | Self-update policy for the worker binary. `auto` downloads and atomically swaps the binary on a new platform release (old binary kept at `<exe>.bak`); manual only prints a nudge. On demand there's `mago update [--check] [--force]` — see `docs/SELF-UPDATE.md`. Overridable live via `mago mode update=auto|manual`. The binary must live in a directory writable by the worker user (`mago install` → `~/.local/bin`). |

---

## Verification & merge gating

By default, a PR is gated on the model's own judgment. These options add an objective
check and control when an approved PR is auto-merged.

| Variable | Default | What it does |
| --- | --- | --- |
| `MAGO_VERIFY` | *(off)* | Set to `1` to auto-detect and run a verification command (e.g. `go test` for Go) before approval. |
| `MAGO_VERIFY_CMD` | *(unset)* | An explicit verification command, e.g. `"npm test"`. Takes precedence over auto-detection and implies verification is on. |
| `MAGO_NO_MERGE` | *(off)* | Set to `1` to never auto-merge — the agent approves and a human merges. |
| `MAGO_MERGE_UNVERIFIED` | *(off)* | Set to `1` to auto-merge even when no verification check could run. Without it, an approved-but-unverified PR is left for manual merge. |

---

## Beyond-code communications

| Variable | Default | What it does |
| --- | --- | --- |
| `MAGO_COMMS` | *(off)* | Set to `1` to enable the beyond-code loop: when an implementer PR (`mago/task-*`) merges, the CMO drafts a non-engineering deliverable (e.g. an announcement). |

---

## Worker & event-driven serving

`mago serve` runs an event-driven worker that GitHub webhooks wake to reconcile.

| Variable | Default | What it does |
| --- | --- | --- |
| `MAGO_WORKER_ID` | the machine hostname | Identifies this worker. One account may run several workers serving different repos. |
| `MAGO_WEBHOOK_SECRET` | *(unset)* | HMAC secret used to verify inbound GitHub webhook signatures. |

`mago serve` flags:

| Flag | Default | What it does |
| --- | --- | --- |
| `--addr` | `:8099` | Address the webhook listener binds to. |
| `--secret <hmac>` | *(unset)* | HMAC secret (alternative to `MAGO_WEBHOOK_SECRET`). |
| `--heartbeat <secs>` | — | Heartbeat interval. |
| `--relay` | off | Dial out to the platform's webhook relay instead of exposing a tunnel (NAT-friendly). |
| `--daemon` | off | Detach a supervisor that keeps the worker running (pidfile + log, restart on crash). Control it with `mago serve stop` / `mago serve status`. |
| `--until HH:MM` | — | Stop cleanly at the next occurrence of that wall-clock time (24h) — a native scheduled stop, no cron kill needed. |
| `--start-delay <dur>` | — | Wait this duration (e.g. `15m`, `900s`) before serving — staggers fleet workers without an OS `sleep` wrapper. |

---

## Platform & account

These affect the `register` / `login` / `subscribe` / `account` commands and the relay.

| Variable | Default | What it does |
| --- | --- | --- |
| `MAGO_PLATFORM_URL` | `http://localhost:9100` | Platform API base URL. |
| `MAGO_PASSWORD` | *(unset)* | Non-interactive password for `register` / `login`. |

Account state is persisted at `~/.mago/config.json` (mode `0600`), written by the
account commands:

```json
{
  "platform_url": "https://...",
  "email": "you@example.com",
  "token": "<JWT from the platform>",
  "license_key": "<issued on first payment; used by the worker>"
}
```

---

## CLI flags

| Flag | Applies to | Default | What it does |
| --- | --- | --- | --- |
| `-C <dir>` | all company commands | cwd, or `$MAGO_COMPANY` | Company directory to operate on. |
| `--project <p>` | `task add` | *(none)* | Attach the new task to a specific project. |
| `--repo owner/repo` | `project add` | — | Repo for the project being registered. |
| `--mirror` | `project add` | off | Opt-in: mago opens a tracking issue on the project repo that the deliverable PR closes (stored as `mirror_issue` in `.mago/projects.json`). |
| `--base <secs>` | `loop` | `3` | Cadence interval when the last tick did work. |
| `--max <secs>` | `loop` | `60` | Upper bound the interval doubles toward while idle. |
| `--max-ticks <n>` | `loop` | `5` | Number of ticks before the loop stops. |

---

## Config files at a glance

| Path | Written by | Holds |
| --- | --- | --- |
| `~/.mago/config.json` | `mago register` / `login` / `subscribe` | platform URL, email, JWT token, license key |
| `~/.config/tau/config.json` | tau | provider API keys (`keys` map, or global `api_key`) |
| `<company>/.mago/usage.json` | the worker | daily work-cycle usage for `MAGO_DAILY_BUDGET` |
| `<company>/.mago/agents/*.md` | `mago init` | per-agent provider/model and role prompts |

---

## Internal / advanced

These are used for development, packaging, or smoke tests — most operators never set them.

| Variable | What it does |
| --- | --- |
| `MAGO_PLATFORM_ENV` | Path to a `.env` the platform loads first (dev). |
| `MAGO_BIN_DIR` | Install destination for the CLI installer script (default `$HOME/.local/bin`). |
| `MAGO_CLI_DIR` | Directory the platform serves CLI binaries from (`mago-<os>-<arch>`). |
| `MAGO_CLI_BINARY` | Legacy single-file CLI binary fallback for `linux/amd64`. |
| `FEEDBACK_RELAY` | Override the default feedback relay URL (default `https://feedback.intrane.fr`), or set to `off` to disable the relay write. |
| `MAGO_FEEDBACK_REPO` | Platform-side `owner/repo`. When set, `mago feedback` submissions are also filed as `feedback`-labeled triage issues on that repo via the GitHub App (never `mago`-labeled, so they are not auto-implemented). |
| `MAGO_TEST_BAD_REFLECTION` | Test hook that forces a malformed reflection. |
