# Roadmap

> **Status (2026-06):** v1 is **shipped and live at mago.intrane.fr** — worker runtime
> (git-native tick, tau driver, GitHub backend, memory/skills, HITL, role routing, adaptive
> cadence, multi-project, `mago-state`), the **two-module split**, and the **platform backend**
> (accounts, **live Stripe** + 48h no-card trial + billing portal, GitHub App webhook relay,
> repo entitlement), plus the public install path, operator/agent guides (`/operators`,
> `/llms.txt`), and onboarding observability (`mago-platform activity`). The autonomous loop is
> proven end-to-end — **20+ merged `mago/*` PRs** across real repos. Next: **v1.5 — from
> task-executor to company-operator** (below).

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

## v1.5 — from task-executor to company-operator

The v1 loop is proven: agents pick up issues, ship PRs, review, merge. The gap to the north
star isn't "can agents ship code?" — it's that agents don't yet **decide what to do**, work
**only on code**, and need a human at each step. This phase closes that gap. Each item is
event- or cadence-driven and lands as GitHub artifacts (issues/PRs/comments), keeping the
"repo is the company" and "transparent by default" principles.

- **Beyond code — non-engineering loops** *(in progress)*. Company **events**, not human issues,
  trigger non-code work: when a feature PR merges, the **CMO** autonomously produces comms — a
  `CHANGELOG.md`/release-note entry, an announcement draft — shipped as its own deliverable.
  Proves the company is more than a code bot. Opt-in per repo so it's safe.
- **Proactive backlog — agents set the agenda** *(shipped)*. With `MAGO_PROACTIVE=<secs>`, the
  **Head of Product**, given the mission in `STATE.md`, proposes and files issues itself on a
  cadence — capped (≤2/cycle, stops at 3 active) and deduped — instead of waiting for the CEO to
  file every task. The biggest step from "executes tasks" to "runs the company." (`backlog.go`)
- **Autonomy & trust guardrails** *(shipped: budget + digest)*. `MAGO_DAILY_BUDGET=<n>` caps
  autonomous work cycles per UTC day (worker pauses when hit; `budget.go`); `mago digest` shows the
  day's backlog/PRs/HITL + budget usage (`digest.go`). Still open: cost/token-based budgets,
  push delivery of the digest (Telegram — javimosch/mago#10), and auto-escalation on repeated fail.
- **Dogfood — mago runs mago** *(set up, review-only)*. A real mago company (`~/ai/mago-company`,
  internal account) operates `javimosch/mago`: planner proposes → CTO PRs → reviewer comments →
  CMO announces; scoped to a docs/DX/tests mission, label-scoped, budget-capped, `MAGO_NO_MERGE` so
  PRs wait for human merge. Cheapest proof of "runs a company" + real metrics. See `docs/DOGFOOD.md`.
- **First real user.** One friendly real customer — demand teaches more than dogfooding, and
  surfaces the gaps no internal run will.

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
