# Roadmap

> **POC status:** the v1 **worker runtime** below (git-native tick, triggers, tau driver,
> GitHub backend, memory/skills, HITL, role routing, adaptive cadence, multi-project,
> state-on-`mago-state`) is built and verified — see [STATUS.md](STATUS.md). What remains
> for v1: the **platform backend** (accounts, Stripe, webhook relay), the **two-binary
> split**, real **project clone→PR**, and git **worktrees**.

## v1 — a single company, run cheaply

Goal: a client's own agent can onboard, stand up one worker and one company, and that
company autonomously works its project repos via GitHub, escalating to the human only when
needed.

**Platform backend (operator, private binary)**
- Accounts with **email/password** auth.
- Stripe single plan (€20/mo): checkout link via CLI, subscription webhooks, license check.
- GitHub **webhook relay**: ingress for repo events, relays down the persistent connection
  to the right worker.

**Client binary (CLI + worker, bundled)**
- Account ops: `register`, `login`, `subscribe`, `account status`.
- Worker ops: `worker add`, `worker doctor` (auto-configure/validate **tau** + **gh**),
  `worker start/stop`.
- Company ops: `company create` (creates repo, scaffolds `.mago/`, cuts `mago-state`,
  installs webhooks, seeds the **executive team**: CTO, CMO, Head of Product, Head of Org
  Engineering — reporting to the CEO/client).
- Project ops: `project add` (grant + register a project repo; agents reach any of them).
- Agent hands: `mago ask` (HITL via issue comment).

**Worker runtime**
- Git-native reconcile loop over the company repo.
- Triggers: **manual**, **cron**, **adaptive cadence**.
- tau driver: single-shot **json+stream** subprocess; goal sessions for memory.
- Per-agent worktrees; append-only journals on `mago-state`.
- Collaboration via **GitHub only** (issues = tasks, comments = HITL, PRs = deliverables).
- **Skills as memory**: agents read/use/improve `.mago/skills/` (learnings, caveats,
  pitfalls, gotchas). Decisions recorded durably and respected.
- Cost dials: cheap-model defaults, adaptive cadence, per-agent token budgets.

**Scope limits for v1**
- **One worker per account.**
- email/password only (no GitHub login yet).
- No a2a bus, no web panel, no ACP transport.
- The fixed executive team (no dynamic hiring yet — see v2).

## v2 — scale and polish

- **GitHub login** (replaces email/password) and a **GitHub App** (fine-grained tokens,
  one central webhook ingress, bot identity).
- **Multiple workers per account** with load distribution.
- **Hiring** — the **Head of Org Engineering** grows from maintaining the company to
  staffing it: writing new agent definitions (PRs to `main`) when the backlog needs a role
  that doesn't exist. The company hires itself.
- Richer observability (run history surfacing, cost summaries per run).
- ACP transport behind the existing `Driver` interface for live/interactive runs.
- Webhook latency hardening; self-hosted worker ingress options.

## Horizon

- A library of reusable agent/company templates clients configure rather than build.
- Companies trustworthy enough to run real operations with minimal human oversight.
