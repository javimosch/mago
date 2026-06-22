# AGENTS.md — coding guidelines for the mago repo

How to write code in this repo. For **how mago actually works** (architecture, usage, live
testing, SaaS vs core), read the skills in [`.agents/skills/`](.agents/skills/) and the design
docs in [`docs/`](docs/) — start with [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md),
[`docs/VISION.md`](docs/VISION.md), [`docs/SAAS.md`](docs/SAAS.md), [`docs/DEPLOY.md`](docs/DEPLOY.md).

## The one rule that matters most: core stays stdlib-only

This repo is **two Go modules**, and the boundary is load-bearing:

| Module | Path | `go` | Deps | Visibility |
|---|---|---|---|---|
| **core** (`mago`) | repo root | 1.22 | **zero external** (stdlib only) | open-sourceable |
| **platform** (`mago-platform`) | `platform/` (own `go.mod`) | 1.25 | bcrypt, modernc sqlite | operator-private |

- **Never add a third-party dependency to the core module.** If you reach for one, either use
  the stdlib or put the code in `platform/`. The core's value is that it's auditable, dependency-free,
  and publishable. `go list -m all` in the root must show only `mago`.
- The core never imports `platform/`. The platform is a separate binary + module; removing the
  `platform/` directory must leave a clean, buildable core.
- Build: `go build -o mago .` (core, from root) · `cd platform && go build -o ../mago-platform .`
  (platform). Static deploy build: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`.

## Agent-first CLI

mago is driven by agents (the operator's own agent onboards; mago's worker agents call `mago`
and `gh` via bash). Design commands for a machine caller:

- **stdout** = results/data only; **stderr** = logs, progress, errors.
- Idempotent, scriptable, no interactive prompts on the happy path (credentials come from flags,
  `$MAGO_PASSWORD`, or a no-echo prompt only as a last resort).
- The CLI must be legible enough for an LLM to drive from `mago help` alone — this is tested by
  the operator simulation (see `.agents/skills/live-testing.md`). Confusing help/usage is a bug.

## Go conventions

- **Max ~500 LOC per Go file.** Split by responsibility (extract, don't append). Each file is
  single-purpose — see the existing split: `account.go` (platform-API client), `tick.go` (one
  agent tick), `route.go` (task→agent), `serve.go` (event loop), `git_state.go` (mago-state),
  `relay.go` (worker dial-out), `review.go` (PR review); platform: `store.go`, `stripe.go`,
  `auth.go`, `relay.go`, `github.go`, `daemon.go`, `setupgithub.go`.
- `gofmt` everything. Match surrounding style, comment density, and error-handling idiom.
- Comments explain **why**, not what. Keep them where the existing code keeps them.

## Daemon / process surface

`mago-platform` (operator) has the daemon lifecycle: `start [--daemon] [--port]`, `stop`,
`status` (`platform/daemon.go`). Keep daemon/Stripe/webhook-relay/secret logic in `platform/`
only — the `mago` client binary must never expose a platform control surface.

## Semantic exit codes

```
0       success
80–89   user errors (invalid input, permission denied)
90–99   resource errors (not found, already exists)
100–109 integration errors (network, timeout, GitHub/Stripe/tau failures)
110–119 software errors (internal, unexpected)
```

Scripts/agents branch on `$?`; keep the mapping stable.

## Keys & secrets

- **BYOK invariant:** the client's LLM provider key never leaves the worker and is never sent to
  the platform.
- Client config `~/.mago/config.json` (`0600`); platform secrets in `platform/.env` (gitignored,
  never committed) and on the server beside the binary.
- Never log or commit tokens/keys. Before every commit, scan the staged diff for `sk_`, `whsec_`,
  `ghp_`, `-----BEGIN`, etc. `platform/.env`, `.env`, and `*.db` are gitignored — keep them so.

## External tools

The worker shells out to **tau** (agent harness) and **gh** (GitHub). They must be installed +
authenticated; fail with a clear `100–109` integration error, not a crash, when missing.

## git-native state

- Agent **definitions** live on `main`; **runtime exhaust** (STATE.md, `.mago/runs|skills|memory|
  inbox`) on the orphan `mago-state` branch (`git_state.go`). GitHub issues = tasks, labels =
  status (`mago:in-progress`, `agent:<name>`, `project:<name>`), PRs = deliverables.
- Journals are append-only timestamped files. `pushState` stages **only** runtime paths (never
  `git add -A`) and never lets definitions leak onto `mago-state`.

## Docs discipline

- Every feature ships a short doc under `docs/` (kebab-case). Keep `docs/ARCHITECTURE.md` and
  `docs/SAAS.md` authoritative; update them when a locked decision changes.
- When mago's behavior changes in a way that affects how agents operate it, update the matching
  skill in `.agents/skills/`.
