# mago

**Cheap autonomous agents that run companies.**

mago is an agent-first platform for autonomy-level-3 **agents** — workers that run
24/7 on schedules and cadences, do real work without a human watching each step, and
escalate to a human only when needed. A *company* is a GitHub repo, and **you are its
CEO**: mago seeds an executive team — CTO, CMO, Head of Product, Head of Org Engineering —
that takes its tasks from the repo's issues, works across the company's project repos,
ships pull requests, and discusses in comments. They read and improve shared **skills**
(learnings, gotchas) so the company gets cheaper and better over time. You run it all
from GitHub.

mago does **not** sell LLM completions. You bring your own key (BYOK); your worker runs
on your machine with your provider key, and your work lives in your own GitHub account.
mago provides the orchestration.

## How it works (one breath)

```
You ──(via your own agent, e.g. Claude Code)──▶ mago CLI
   register · subscribe · add worker · create company · add projects

Worker (your machine) ── runs agents via tau ──▶ your GitHub repos
   reads .mago/agents/*.md · takes issues · opens PRs · journals to mago-state

Platform backend (ours) ── accounts + Stripe + webhook relay
   GitHub webhook ──▶ platform ──▶ your worker (dials out, NAT-friendly)
```

## Status

**Working POC.** A single `mago` binary runs the core loop end to end on real models —
memory/progression, claims, HITL (local + GitHub), role routing, adaptive cadence,
multi-project, and state pushed to a `mago-state` branch — with no platform, accounts, or
billing yet. See [docs/STATUS.md](docs/STATUS.md) for exactly what's built.

```sh
go build -o mago .
./mago init myco
./mago task add "Build a /health endpoint with a test" -C myco
MAGO_PROVIDER=opencode-go MAGO_MODEL=deepseek-v4-flash ./mago run cto -C myco
# GitHub mode: set MAGO_GH_REPO=owner/repo (tasks become issues, HITL via comments)
```

## End-to-end example

A full local run, from an empty directory to a company that opens pull requests. Each
command is independent and idempotent — re-running is safe.

```sh
# 0. Build the binary (core is stdlib-only, no external deps to fetch)
go build -o mago .

# 1. Scaffold a company. Creates .mago/, STATE.md, tasks/, workspace/, projects/.
./mago init myco

# 2. Register a project repo the agents will ship PRs to (shorthand: owner/repo)
./mago project add javimosch/mago -C myco
./mago project list -C myco

# 3. Add a task. It becomes a unit of work the router assigns to a best-fit agent.
./mago task add "Build a /health endpoint with a test" --project javimosch/mago -C myco

# 4a. Run ONE tick of a specific agent (brief -> tau -> reflect -> write back)
MAGO_PROVIDER=opencode-go MAGO_MODEL=deepseek-v4-flash ./mago run cto -C myco

# 4b. ...or let the router pick agents for all open tasks and run each
./mago tick -C myco

# 5. Inspect progress: STATE.md, tasks, and any pending human-in-the-loop questions
./mago status -C myco
./mago digest -C myco        # "what your company did": backlog, PRs, HITL, budget

# 6. If an agent escalated a decision (needs_human), answer it so the task resumes
./mago answer <task-id> "Yes, use the stdlib net/http mux" -C myco
```

GitHub mode wires the same loop to a real repo: set `MAGO_GH_REPO=owner/repo` and tasks
become issues, HITL happens in issue comments, and PRs open against that repo. Before a
worker runs unattended, validate its environment:

```sh
./mago worker doctor          # checks tau, gh, and OPENCODE_API_KEY; exits 101 on any failure
```

For a long-running worker, replace the one-shot `run`/`tick` with a cadence or the
event-driven server:

```sh
./mago loop cto -C myco       # adaptive cadence (--base/--max/--max-ticks seconds)
./mago serve -C myco          # GitHub webhooks wake a reconcile (--addr, --secret, --relay)
```

## Troubleshooting

### `mago worker doctor` reports a failed check

Run it first whenever a worker behaves oddly — it pinpoints a missing dependency before a
tick burns tokens:

- **`tau (LLM driver) on PATH` fails** — install [tau](https://opencode.ai) and ensure it
  is on your `PATH`. tau is the stateless per-tick LLM driver; without it `run`/`tick`
  cannot call a model.
- **`gh (GitHub CLI) on PATH` / `gh authenticated` fails** — install
  [gh](https://cli.github.com) and run `gh auth login`. Required for GitHub mode (issues,
  comments, PRs).
- **`OPENCODE_API_KEY set` fails** — export your provider key (BYOK). mago never ships
  completions; the key bills you directly.

### Formatter errors (`gofmt`)

The repo requires every Go file to be `gofmt`-clean (see [AGENTS.md](AGENTS.md)). CI and
reviewers reject unformatted code, and a worker's PR can fail verification on it.

```sh
gofmt -l .                    # lists files that are NOT formatted (empty output = clean)
gofmt -w .                    # rewrites them in place
```

Common causes: tabs-vs-spaces (Go indents with **tabs**, not spaces), an extra blank line,
or misaligned struct fields / `const` blocks. `gofmt -d <file>` prints the exact diff it
wants. Most editors can run `gofmt`/`goimports` on save to avoid this entirely.

### Linter / build errors (`go vet`, `go build`)

mago's verify step (and any sane CI) runs `go build ./... && go test ./...`; `go vet`
catches a class of bugs the compiler allows. Run all three locally before pushing:

```sh
go vet ./...                  # suspicious constructs: bad Printf verbs, unreachable code, lost locks
go build ./...                # must compile cleanly
go test ./...                 # must pass
```

Frequent offenders:

- **`Printf format %d has arg x of wrong type string`** — mismatched verb and argument;
  fix the verb or the value. `go vet` flags these even though the code compiles.
- **`declared and not used` / `imported and not used`** — Go treats unused locals and
  imports as **compile errors**, not warnings. Delete them (or use `_` for a deliberately
  unused import side-effect).
- **`undefined: X`** — a symbol in another file of the same package was renamed or removed;
  build the whole package with `go build ./...`, not a single file.
- **Adding a third-party import to the core module** — this is rejected by design. The core
  module (repo root) is **stdlib-only**; put dependency-using code in `platform/`. Verify
  with `go list -m all` — the root must show only `mago`. See [AGENTS.md](AGENTS.md).

Reproduce exactly what the merge gate sees by enabling verification on a checkout:

```sh
MAGO_VERIFY=1 ./mago ...                       # auto-detects Go and runs build + test
MAGO_VERIFY_CMD="go vet ./... && go test ./..." ./mago ...   # or supply your own command
```

Docs:

- [STATUS.md](docs/STATUS.md) — what's actually built vs. designed
- [VISION.md](docs/VISION.md) — what mago is and the bet behind it
- [ARCHITECTURE.md](docs/ARCHITECTURE.md) — the layers, binaries, data flow, git-native model
- [MEMORY.md](docs/MEMORY.md) — how short ticks accumulate into real progress
- [ROADMAP.md](docs/ROADMAP.md) — v1 scope and what waits for v2
- [AGENTS.md](AGENTS.md) — coding guidelines for working in this repo

## Pricing

Single plan, €20/month. BYOK — your LLM provider bills you for tokens directly.
