# Architecture

This document is the locked-in design for mago v1. Decisions here are committed; open
questions are called out explicitly at the end.

## Layers

```
L0  Platform backend (operator-owned, ours)
      accounts (email/password) · Stripe subscriptions · GitHub webhook relay
      public IP. Holds no config, no compute, no LLM keys.
        ▲ login / subscribe link / account status / license check
        │ persistent connection (worker dials out)
L1  Client machine
      • mago CLI      account / worker / company / project ops — agent-first
      • worker        the git-native runtime: reconcile loop, runs tau, drives gh
      (CLI + worker ship in ONE client binary)
        requires tau + gh installed and configured (mago auto-configures/validates)
L2  Client's own agent (Claude Code / "hermes")   NOT part of mago
      drives the mago CLI on the human's behalf — does onboarding conversationally
L3  Company   = a GitHub repo
      .mago/agents/*.md (defs) · issues = tasks · comments = HITL · mago-state branch
L4  Projects  = GitHub repos
      the actual product codebases the company's agents work on
      (the worker's gh must have access to each)
```

## Binaries (split — locked)

The operator's platform control must never be exposed to clients, so binaries are split:

| Binary | Owner | Contains | Notes |
|---|---|---|---|
| **platform** | operator (private) | backend daemon, accounts, Stripe, webhook relay; `daemon start/stop` | never distributed to clients |
| **mago** | client (public) | CLI + worker (bundled) | the only thing a client installs |

Mixing CLI + worker in the client binary is intentional and fine. Mixing the platform
daemon in is not — it stays a separate, private build.

## Hierarchy

```
Account (email/password)
  └── Worker            (one per account in v1; multiple in v2)
        └── Company      (a GitHub repo — the orchestration hub)
              └── Project (a GitHub repo — a product codebase)   [many per company]
```

A company is **one GitHub repo** that holds an executive team and a list of **N project
repos** (the actual codebases). Clients often run one company per product, but a company
can hold many repos. Company agents may interact with **any** of the company's project
repos via the `gh` CLI **at their own discretion** — driven by their role, recorded
decisions, and skills, not by a fixed per-agent assignment. The worker's `gh` credential
must have access to the company repo and all its project repos.

## The org (starter team)

The **client is the CEO** — not an agent. The CEO sets direction by filing issues and
answering HITL escalations. `company create` seeds an **executive team** reporting to the
CEO:

| Agent | Role |
|---|---|
| **CTO** | engineering across all project repos — triages technical issues, implements, reviews and merges PRs |
| **CMO** | marketing & growth — positioning, content, outreach |
| **Head of Product** | turns CEO intent into specs and prioritized issues; owns the backlog |
| **Head of Org Engineering** | maintains the company itself — agent definitions, skills, cadences, internal automation (the seed of v2's hiring meta-agent) |

Each head is one agent in v1. They coordinate as peers through GitHub (issues/PRs/comments)
with the worker as the single scheduler — no separate bus. An agent decides what to act on
from three durable inputs:

- **Roles** — the agent definitions (`.mago/agents/*.md`): who does what.
- **Decisions** — durable company decisions the agents must respect (recorded as
  `mago:decision`-labeled issues / ADR notes under `.mago/decisions/`).
- **Skills** — learned knowledge they read, apply, and improve (see *Skills* below).

## The runtime: a git-native reconcile loop on the worker

The worker is the company's runtime. There is no panel and no central scheduler — the
worker reads the company repo and reconciles it, operator-style:

- **Desired state** = `.mago/agents/*.md` on the company repo's `main` (human-curated).
- **Observed state** = run journals on the `mago-state` branch + open issues/PRs/comments.
- **Reconcile** = for each agent that is *due* (cron/cadence) or *pinged* (a relayed
  webhook event), wake it, run one tau turn, record the journal, re-schedule.

Everything reduces to a single event queue with producers:

```
GitHub webhook ─(via platform relay)─┐
cron fire                            ├──▶  [ wake(agent, reason) ]  ──▶  Runner  ──▶ tau
cadence timer                        │
manual (mago CLI / UI)               ┘
```

### Driving tau

- **Single-shot `json+stream` subprocess per tick** (locked). Each wake is one invocation:
  `tau --session <agent> --tools … --schema … "<prompt>"`, read the NDJSON stream, done.
- Goal sessions carry the agent's memory across runs (`/goal …`, pause/resume/status).
- The tau driver sits behind a `Driver` interface so ACP can be added later for live runs.

### Agent hands = bash → CLIs on PATH

Agents act only through tau's `bash` tool, calling CLIs the worker puts on PATH:

- `gh` — read/triage/assign issues, open PRs, comment, merge on the project repos.
- `mago` — platform-aware helpers, e.g. `mago ask "<question>"` to escalate to the human
  (opens a `mago:hitl` issue comment, pauses the goal).

No changes to tau's tool registry are required.

### Collaboration = pure GitHub (a2a dropped)

There is no separate agent-to-agent bus. The worker is the single coordinator, so peers
never need to negotiate directly, and all coordination stays human-visible:

- **Issues** = backlog. Labels (`mago:task`, `mago:hitl`, `agent:<name>`) and assignees
  carry state. **The issue tracker is the task system.**
- **PRs** = deliverables; a reviewer agent reviews and merges.
- **Comments** = discussion and HITL. The inbox is just filtered issues.

## State layout (git-native)

Agent definitions live on `main` (humans curate and PR them). All runtime exhaust lives on
an orphan **`mago-state`** branch so the product history stays clean.

```
company-repo/  (main)
  STATE.md                   the company brain: goals, shipped, in-flight, decisions
  .mago/
    config.json              company config (defaults, budgets)
    projects.json            the N registered project repos
    agents/
      cto.md                 frontmatter + persona prose
      cmo.md
      head-of-product.md
      head-of-org-engineering.md
    skills/                  shared, improvable memory (see Skills + docs/MEMORY.md)
      INDEX.md               one line per skill; always in context
      <skill-name>/SKILL.md
    decisions/               durable decisions agents must respect (ADR-style)
      <id>.md

company-repo/  (mago-state, orphan branch)
  runs/<agent>/<ts>.json     machine: trigger, tokens, tools, outcome, next-cadence
  runs/<agent>/<ts>.md       human: narrative
  state/<agent>.json         cursors: last-seen issue, goal status
  memory/<agent>/*.md        long-term notes
```

### Agent definition (the platform's real API)

```yaml
name: cto
title: "Chief Technology Officer"
reports_to: ceo
model: deepseek-chat                 # cheap by default; client's key (BYOK) pays
trigger: cadence(min=10m, max=12h)   # or cron("0 */2 * * *") | manual
budget: { daily_tokens: 5_000_000 }
tools: [bash, read, write, edit]
# --- persona prose below the frontmatter ---
You are the CTO. You own engineering across all of the company's project repos. You
triage technical issues, implement or delegate, and review and merge PRs. Before acting,
read the relevant skills; after learning something, improve them. …
```

There is no per-agent `projects:` field — agents reach any company project repo via `gh`
at their discretion. The canonical list of project repos is company-level
(`.mago/projects.json`).

### Concurrency safety

Git has no row locks, so conflicts are made structurally impossible:

1. **Per-agent worktrees** — each run gets its own worktree; tau's `cwd` points at it.
2. **Append-only journals** — a run writes a new timestamped file, never edits one.
3. **Per-agent path ownership** — an agent writes only under its own `runs/state/memory`.
4. **Worker is sole writer to `main`** — agents propose; shared *code* conflicts resolve
   via PRs + the reviewer agent (conflict becomes review, not failure).

## Skills (shared, improvable memory)

Skills are the company's living memory: markdown notes capturing **learnings, caveats,
pitfalls, and gotchas** that make future runs faster and safer. They live at
`.mago/skills/<name>/SKILL.md` and are shared across all agents (not role-scoped).

The loop every agent follows:

1. **Read** — at the start of a run, consult skills relevant to the task before acting.
2. **Use** — apply what they say (a known pitfall avoided is tokens and a failed PR saved).
3. **Improve** — when an agent learns something (a gotcha, a fix, a better approach), it
   refines an existing skill or writes a new one.

Each skill is a single focused fact/recipe with light frontmatter (name, description,
when-to-use), mirroring the boilerplate's `.agents/skills` and Claude Code's skills/memory
pattern. Skills are improved by the worker (the sole writer), keeping them conflict-free;
they are durable knowledge, so they live on `main` alongside agent definitions — not in the
per-run `mago-state` exhaust. This is how a mago company gets cheaper and better at its job
over time without the human curating every lesson.

## Memory & progression

How short, stateless ticks add up to real progress — and how agents avoid repeating
mistakes, redoing finished work, or overlapping. Full design in [`docs/MEMORY.md`](MEMORY.md).
The load-bearing rules:

- **The world is truth; the session is scratch.** Every run re-derives "where am I" from
  the backend (issues/PRs/git or the local backend), never from the goal session.
- **Five memory types, five homes:** task (the issue/PR thread), world (`STATE.md`),
  lessons (skills), episodic (journals), working (goal session).
- **Context assembly** builds a *briefing* before each run (role · `STATE.md` · active task ·
  `skills/INDEX.md` · recent journals · decisions). The agent reads it before acting.
- **Claims** = self-assign + `agent:<name>` label + a claim comment. The reconciler won't
  re-dispatch a claimed, active task. Stale claims auto-release. (This is why no a2a bus.)
- **Reflection is structural:** every run ends with a schema-forced output
  (`summary`, `state_delta`, `task_status`, `lessons[]`, `next`, `cadence_signal`) and the
  *worker* does the bookkeeping from it — journal, STATE.md, skills, claim, HITL, cadence.

## World backend (GitHub / Local) — the seam that enables the POC

"Tasks / discussion / HITL / deliverables / claims / durable state" is an **interface**, not
GitHub specifically. Two implementations:

| Capability | Production (GitHub) | Local POC |
|---|---|---|
| tasks | issues | `tasks/*.md` files |
| discussion / HITL | issue comments | local inbox file + CLI prompt |
| deliverables | pull requests | local branches / diffs |
| claims | assignee + label | a field in the task file |
| durable state | commits (+ push) | local commits, **no push** |

The core (reconcile loop, context assembly, claims, reflection, skills/STATE.md, tau driver)
runs identically against either backend. v1 ships the GitHub backend; the **POC ships the
Local backend** so the whole model is smoke-testable with no GitHub, push, platform, or SaaS.

## Webhooks & HITL latency (relay model — locked)

Workers have no public IP and that is fine. The platform backend has the public IP and is
the webhook ingress:

```
GitHub repo event ──▶ platform backend ──▶ (down the persistent connection) ──▶ worker
worker acts ──▶ writes back into the client's GitHub repo
```

This gives seconds-latency HITL behind NAT: when the human answers a `mago:hitl` comment,
GitHub notifies the platform, which relays it to the worker, which resumes the paused agent.
mago auto-creates the repo webhooks pointing at the platform during `company create`.

## BYOK & keys

- **LLM provider key** — lives only on the worker (env, whatever tau's provider needs).
  Never touches the platform. This is "we don't provide completions."
- **GitHub access** — email/password account in v1; the worker's `gh` holds repo access.
  v2 moves to GitHub login / App (which also centralizes the webhook ingress).

## Cost dials (now client-facing, not operator margin)

Because tokens are BYOK, "cheap" protects the *client's* bill, not mago's margin:

- **Adaptive cadence** — idle agents back off toward their max interval → ~zero spend.
- **Cheap-model defaults** — per-agent `model:`; reserve strong models for judgment roles.
- **Token budgets** — per-agent daily/weekly ceilings; agent pauses when exhausted.

## Onboarding flow (agent-driven)

The human's own agent drives these; the mago CLI is agent-first (JSON out, semantic exits):

```
mago register --email you@co.com      create account
mago subscribe                        prints Stripe checkout link; pay; status → active
mago account status --json
mago worker add <name>                register a worker on this machine
mago worker doctor                    check/auto-configure tau + gh (auth, provider key)
mago company create acme              worker's gh creates the company repo, scaffolds
                                      .mago/, cuts mago-state, installs webhooks, seeds team
mago project add acme/api             grant + register a project repo
# from here: the worker reconciles; the human files issues and answers HITL on GitHub
```

## Tech stack

- **Go** for both binaries, built from the boilerplate scaffold (single static binary,
  embedded assets, stdlib HTTP, daemon via PID file + signals).
- **tau** (external) as the agent harness, driven as a subprocess in json+stream mode.
- **gh** (external) as the GitHub interface on the worker.
- **Stripe** for billing (checkout link via CLI; webhooks to the platform backend).
- Local client config at `~/.mago/config.json` (0600), rcmd-style.

## Open questions (not yet locked)

- **email/password store** on the backend — hashing/storage choice; v2 supersedes with
  GitHub login.
- **Worker reconcile cadence** — the worker's own heartbeat interval (its cheapness dial).
- **Skill retrieval at scale** — `skills/INDEX.md` + description match is enough for
  hundreds; ranking/embeddings is a later concern (see `docs/MEMORY.md`).
- **Decisions mechanism** — `mago:decision` issues vs `.mago/decisions/` ADR files vs both,
  and how agents are reliably made to respect them.
- **Exec personas** — the actual persona prose for CTO / CMO / Head of Product / Head of
  Org Engineering seeded by `company create`.
- **POC backend shape** — the exact local `tasks/*.md` schema and inbox format for the
  smoke-test build.
