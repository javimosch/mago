# Agent runtime — how the company runs

A mago **company** is a directory with `.mago/` (run `mago init`). Its agent team does work
driven by GitHub issues; the **worker** (`mago serve`) is the process that runs the agents.

## The team

`mago init` seeds an executive team (the human is the **CEO**):
- **cto** — engineering; implements tasks as PRs (never merges its own work).
- **cmo** — marketing/docs/copy.
- **head-of-product** — turns intent into specs/well-scoped tasks.
- **head-of-org-engineering** — `reviews: true`; reviews + merges PRs, never implements.

Agent definitions are markdown with frontmatter (`name`, `title`, `provider`, `model`,
`reviews`) in `.mago/agents/*.md`.

## Ticks (stateless)

`runTick` runs ONE tick for an agent: build a briefing (role + active task + project repo +
recent journals + open PRs) → drive **tau** (single-shot, no session) → parse the agent's
reflection → write back. Memory is files, not context: `STATE.md` (world), `tasks/` or GitHub
issues (task), `.mago/skills/` (lessons), `.mago/runs/` (journals).

The reflection is a single fenced ```json block the agent ends with:
`{summary, state_delta, task_status, lessons[], next, cadence_signal, hitl_question}` where
`task_status ∈ {in_progress, blocked, done, needs_human, reassign, already_done}`. (tau's
`--schema` is NOT used — the provider rejects it; we parse the fenced block client-side.)

## GitHub-native state (`MAGO_GH_REPO` set → githubBackend)

- issue = task; open issue (no agent label) = unclaimed.
- labels = status: `mago:in-progress`, `mago:blocked`, `mago:hitl`, `agent:<name>`,
  `project:<name>`.
- HITL: `mago:hitl` + the question as a comment; the human answers in a comment → resumes.
- PRs = deliverables; merge = done.
- Runtime exhaust → orphan `mago-state` branch; definitions → `main` (see `git_state.go`).

## Reconcile + routing + review

- `reconcileOnce` (serve.go/route.go): route unclaimed open tasks to best-fit agents, then run
  each agent's tick.
- `routeTask` (route.go): a cheap-model call picks the owner from the roster. **Reviewers are
  excluded from issue-task routing** — they only review PRs via the event path. (A task whose
  text mentions "PR/review/merge" must NOT go to the reviewer; that was a real routing bug, now
  fixed in route.go/tick.go.)
- Implementers open a PR in a project-repo clone and must not merge it; the reviewer
  (`review.go` `reviewPR`) judges the diff text against an explicit rubric and merges if correct.
- Guards: deliverable guard (no `done` without a PR), dedup (`already_done`), recovery on
  unparseable ticks, freshness (branch from latest `origin/<default>`), shallow clones.

## Event-driven worker (`mago serve`)

`mago serve` is the worker loop: a GitHub webhook (direct, tunneled, or **relayed** by the
platform) wakes it. `classifyEvent` decides whether to wake and how: `issues opened/reopened`
→ reconcile; `issue_comment` by a human → wake the owning agent; `pull_request opened` →
`reviewPR`; a **merged `mago/task-*` PR** → the CMO drafts a release note (the "beyond code" loop,
opt-in `MAGO_COMMS=1`; `comms.go`). `--relay` makes the worker dial out to the platform instead of
needing a public tunnel (see saas-platform.md). `--heartbeat <secs>` adds a fallback cadence.

## Beyond code (non-engineering loops)

A company **event**, not a human issue, can trigger non-code work. First example: with
`MAGO_COMMS=1`, when a `mago/task-*` PR merges the **CMO** (marketing role — `!implements && !plans
&& !reviews`) writes a short user-facing release note and comments it on the PR. It's terminal (a
mago comment, recognized by `isMagoComment`, so no self-wake) and only fires for `mago/task-*`
branches, so it can't loop. This is the v1.5 "task-executor → company-operator" direction (see
`docs/ROADMAP.md`).

## Proactive backlog (agents set the agenda)

With `MAGO_PROACTIVE=<secs>`, a cadence ticks the **planner** (Head of Product, `plans: true`) to
read the **mission** (`## Mission` in STATE.md) and **file new issues** itself to advance it —
instead of waiting for the CEO to file every task (`backlog.go`). Guardrails: skips if no mission is
set (placeholder `(Set by the CEO…)` counts as unset); proposes at most `proactivePerCycle` (2) per
cycle and never when the active (not-done) backlog is ≥ `proactiveBacklogCap` (3); dedupes against
open + shipped titles. Each proposed issue gets a "📋 Proposed by the Head of Product" comment, then
flows through the normal route → implement → review → merge loop. The full self-running chain:
**planner files an issue → CTO ships a PR → reviewer merges → CMO announces it.**

## Autonomy guardrail (daily budget)

`MAGO_DAILY_BUDGET=<n>` caps autonomous **work cycles** per UTC day (`budget.go`): the worker's run
loop calls `guardBudget()` before each work-producing wake (reconcile / review / release note /
planning) and skips + logs `[budget] … pausing` once the cap is hit, resuming at UTC midnight. Usage
persists in `.mago/usage.json` (resets daily). `mago digest` surfaces it. This makes a company safe
to leave running for days. Still coarse (1 cycle ≈ a few tau calls); token/cost budgets are a TODO.

## Verification-gated autonomy (earned auto-merge)

By default the reviewer judges the *diff* (LLM verdict → comment → merge on approve unless
`MAGO_NO_MERGE=1`). With **`MAGO_VERIFY=1`** (or `MAGO_VERIFY_CMD="<cmd>"`) the worker also **checks
out the PR branch and runs the project's build/tests** (`verify.go`; auto-detects Go →
`go build ./... && go test ./...`, else use `MAGO_VERIFY_CMD`). Policy: a failing check → changes
requested (overrides an LLM approve); **auto-merge requires approve AND green** (or
`MAGO_MERGE_UNVERIFIED=1` to merge when no check exists). So the safe way to run unattended is
`MAGO_VERIFY=1` with `MAGO_NO_MERGE` **off** — only verified-green PRs land. `mago digest` shows
"shipped by mago" (merged `mago/*` PRs in 24h) as the autonomy metric.

## Provider

Agents run via a **harness**. Default is **tau** (`opencode-go` / `deepseek-v4-flash` — matches the
documented BYOK key + tau's default). Override per run/harness with `MAGO_PROVIDER` / `MAGO_MODEL`
(tick.go `applyModelOverrides`) or per-agent frontmatter. Role flags: implementer (CTO)
`implements: true`, planner `plans: true`, reviewer `reviews: true`.

**Claude Code harness** (`claude.go`): set `provider: claude` (model e.g. `sonnet`), or
`MAGO_PROVIDER=claude MAGO_MODEL=sonnet`. Drives the local `claude` CLI in print mode
(`-p --output-format json`); ticks run tools under `--permission-mode bypassPermissions` with the
persona appended via `--append-system-prompt`, and `.result` is parsed as the reflection. **Auth is
the local Claude Code subscription — no API key**, and it sidesteps opencode-go rate limits.
`runTau`/`tauComplete` dispatch to `runClaude`/`claudeComplete` when the provider is `claude`.
**Gotcha:** if the worker runs under a custom `HOME` (e.g. an isolated company dir), set
`CLAUDE_CONFIG_DIR=~/.claude` so claude finds its auth — otherwise every call is "Not logged in"
(the harness now returns that as an actionable error).

**Devin harness** (`debri.go`): set `provider: debri` (model e.g. `SWE-1.6`), or
`MAGO_PROVIDER=debri MAGO_MODEL=SWE-1.6`. Drives devin (a separate agentic coding CLI) through
[debri](https://github.com/javimosch/debri), a small Go wrapper that runs devin in a fresh tmux
session per call and detects completion via devin's own process exit. **Auth is the local devin
login (`devin auth login`) — no API key.** Requires **debri v1.1.0+** (earlier builds could
collide tmux session names under concurrent calls, silently report a crashed/killed session as a
success, and lacked process-exit completion detection) and `tmux` on PATH. Devin has no separate
system-prompt flag, so the persona and the tick briefing are combined into one prompt file passed
via `debri --file`; ticks run under `--permission-mode dangerous` (same posture as `claude`'s
`bypassPermissions`), completions under `--permission-mode auto` (read-only — devin has no true
no-tools mode). `runTau`/`tauComplete`/`agentComplete` dispatch to `runDebri`/`debriComplete` when
the provider is `debri`. **Cost note:** each call spins up a fresh tmux+devin session — heavier
per-invocation than tau/pi/claude. Best for implementer-style ticks (the overhead is amortized over
real coding work); avoid it for routing/planning/review agents that make many small completion
calls. Full bootstrap (installing/authenticating devin, getting debri, verifying with `debri
probe`): see the `a2a-skill` repo's `.agents/skills/a2a-debri-runbook/SKILL.md` — the auth/install
steps are harness-agnostic (mago vs a2a-skill just call the same debri binary differently).

**Event delivery:** the worker's wake channel is buffered (64) and `signal()` drops *coalescable*
recurring wakes (heartbeat/proactive) once it's half full, so a long blocking tick (e.g. a multi-
minute `runClaude` with no streaming) can't let cadence wakes starve one-shot events like a
`pull_request opened` → review. One-shot events (PR review, comms, targeted ticks) are never dropped.
