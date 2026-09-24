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

Platform backend (optional, hosted by us) ── accounts + Stripe + webhook relay
   GitHub webhook ──▶ platform ──▶ your worker (dials out, NAT-friendly)
   Skip it: point a repo webhook straight at `mago serve` instead.
```

## Runs standalone — no account, no licence key

Everything in this repository works on its own. You need a GitHub token, `git`, and an
LLM harness you already pay for. There is no licence check anywhere in this codebase and
nothing phones home.

```sh
go build -o mago .
./mago init myco
./mago task add "Add a /health endpoint with a test" -C myco
MAGO_PROVIDER=claude MAGO_MODEL=sonnet ./mago tick -C myco
```

For GitHub-backed work, point it at a repo and run the worker against your own webhook:

```sh
export MAGO_GH_REPO=owner/repo
export MAGO_WEBHOOK_SECRET=$(openssl rand -hex 20)   # then set the same secret on the repo webhook
./mago serve --port 8099 -C myco                     # POST /webhook/github
```

Label an issue `mago` and the team picks it up. Nothing above touches a hosted service.

**The hosted platform is a convenience, not a requirement.** https://mago.intrane.fr sells
accounts, billing, a GitHub App (so you skip webhook setup) and an outbound relay (so the
worker needs no inbound port) for €20/month. `mago serve --relay` uses it. Every other
command in this repo does not. See [NOTICE](NOTICE).

## Install

```sh
go install github.com/javimosch/mago@latest    # or: go build -o mago .
```

Or take a prebuilt binary from the hosted platform:

```sh
curl -fsSL https://mago.intrane.fr/install.sh | sh
```

`mago` is a single static binary, but it drives tools you supply: **`gh`** (authenticated),
**`git`**, and one **LLM harness** — `mago worker doctor` checks all three before you serve.

Already have the binary somewhere? Relocate it without sudo, and keep it current:

```sh
mago install                 # copy it to ~/.local/bin/mago (--prefix to override)
mago update --check          # exits 5 when a newer build is published
mago update                  # verify -> smoke-test -> atomic swap, keeping a .bak
```

Install it somewhere the running user can write. A root-owned prefix such as
`/usr/local/bin` makes self-update fail for a non-root worker — see
[docs/SELF-UPDATE.md](docs/SELF-UPDATE.md).

## End-to-end examples

### Local company (no GitHub)

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

### GitHub-backed company

```sh
# Tasks become issues; HITL questions go in PR comments
export MAGO_GH_REPO=owner/repo
export MAGO_PROVIDER=opencode-go
export MAGO_MODEL=deepseek-v4-flash
export OPENCODE_API_KEY=sk-...

./mago init myco
./mago task add "Tighten input validation on the signup form" -C myco

# Run the loop; the agent opens a PR when done
./mago loop cto -C myco
```

GitHub mode wires the same loop to a real repo: tasks become issues, HITL happens in
issue comments, and PRs open against that repo. Before a worker runs unattended, validate
its environment:

```sh
./mago worker doctor          # checks tau, gh, and OPENCODE_API_KEY; exits 101 on any failure
```

### Event-driven worker (production)

For a long-running worker, replace the one-shot `run`/`tick` with a cadence or the
event-driven server:

```sh
export MAGO_GH_REPO=owner/repo
export MAGO_VERIFY=1           # run go test before approving a PR
export MAGO_DAILY_BUDGET=50    # cap at 50 work cycles/day

./mago loop cto -C myco        # adaptive cadence (--base/--max/--max-ticks seconds)

# NAT-friendly: dial out to the mago relay instead of exposing a port
./mago serve -C myco --relay

# Or listen on a local port (e.g. behind nginx / cloudflared)
./mago serve -C myco --addr :8099 --secret <hmac-secret>
```

### Claude Code as the agent runtime

```sh
# Run agents using your local Claude subscription — no API key needed
export MAGO_PROVIDER=claude
export MAGO_MODEL=sonnet

./mago run cto -C myco
```

### Answer a blocked task (needs_human)

```sh
# See what needs a decision
./mago status -C myco
#   [needs_human] task 3: Which S3 bucket should test uploads target?

./mago answer 3 "use s3://myco-test-uploads" -C myco
#   task 3 resumed
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
  comments, PRs). For headless servers: `export GH_TOKEN=<your-PAT>`.
- **`OPENCODE_API_KEY set` fails** — export your provider key (BYOK). mago never ships
  completions; the key bills you directly. Alternative: add it to
  `~/.config/tau/config.json` (`{"keys": {"opencode-go": "sk-..."}}`, chmod 600).

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
  module is **stdlib-only** — it has no third-party dependencies at all. Verify
  with `go list -m all` — the root must show only `mago`. See [AGENTS.md](AGENTS.md).

Reproduce exactly what the merge gate sees by enabling verification on a checkout:

```sh
MAGO_VERIFY=1 ./mago ...                       # auto-detects Go and runs build + test
MAGO_VERIFY_CMD="go vet ./... && go test ./..." ./mago ...   # or supply your own command
```

### Agent picks up no tasks / always reports idle

1. Check there are open tasks: `mago status -C myco`
2. In GitHub mode, confirm `MAGO_GH_REPO` matches the repo that has open issues.
3. If `MAGO_TASK_LABEL` is set, the issue must carry that label.
4. The routing logic prefers the named agent — if no task fits the agent's role, it skips.
   Run `mago tick -C myco` to let the router pick the right agent automatically.

### Agents open PRs but they are never auto-merged

Auto-merge is on by default when `MAGO_NO_MERGE` is unset. If PRs sit open:

1. The repo may require branch-protection reviews — the agent's approval satisfies one
   review, but not multiple required reviews or a passing CI check. Set `MAGO_VERIFY=1`
   so the agent runs tests before approving.
2. `MAGO_MERGE_UNVERIFIED` is off by default — if the repo has no detectable check
   command, set `MAGO_MERGE_UNVERIFIED=1` to merge without a check.

### `error: company directory not found` or `init` problems

```sh
# Re-scaffold (safe to re-run; does not overwrite existing state)
./mago init myco

# Or point commands at the right directory
./mago status -C /path/to/myco
# or: export MAGO_COMPANY=/path/to/myco
```

Docs:

- [STATUS.md](docs/STATUS.md) — what's actually built vs. designed
- [VISION.md](docs/VISION.md) — what mago is and the bet behind it
- [ARCHITECTURE.md](docs/ARCHITECTURE.md) — the layers, binaries, data flow, git-native model
- [MEMORY.md](docs/MEMORY.md) — how short ticks accumulate into real progress
- [ROADMAP.md](docs/ROADMAP.md) — v1 scope and what waits for v2
- [CONFIGURATION.md](docs/CONFIGURATION.md) — every env var, config file, and flag that changes how mago runs
- [AGENTS.md](AGENTS.md) — coding guidelines for working in this repo

## Pricing

Single plan, €20/month. BYOK — your LLM provider bills you for tokens directly.

## Licence

Apache-2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE). Use it, fork it, run it, sell
what you build with it.
