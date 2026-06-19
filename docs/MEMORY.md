# Memory & progression

How a mago company avoids the failure modes of autonomous agents — repeating past
mistakes, redoing finished work, two agents overlapping, losing the thread across runs.
This is the subsystem that makes short, stateless ticks add up to real progress.

## The core principle

Each run is a single-shot tick (one tau turn). The trap is treating the tau **goal
session** as the source of truth — sessions drift, compact, and lie. So the rule is:

> **The world (GitHub/git, or the local backend) is truth. The session is scratch.**
> Every run *re-derives* "where am I" from observable state — it never trusts a cached
> belief about its own past.

This is a reconciler, k8s-style: re-read reality each tick, act to close the gap. Short
runs then become a feature — cheap, crash-proof, no long-context rot — not a liability.

Two things must therefore be first-class: **context assembly** (how a run rebuilds its
picture) and **memory typing** (where each kind of knowledge lives).

## Five kinds of memory, five homes

Lumping these together is what makes progression feel unsolvable. Separated, each has a
clear home and a clear read path.

| Memory | Answers | Home | Read path |
|---|---|---|---|
| **Task** | "state of *this* work?" | the task thread (issue + PR) | `gh issue view --comments` / local task file |
| **World** | "what's done / in flight overall?" | `STATE.md` (the company brain) | always in the context pack |
| **Lessons** | "what mistakes/gotchas do we know?" | skills (`.mago/skills/`) | top-K by description match |
| **Episodic** | "what happened, run by run?" | run journals (`mago-state`) | recent N for this agent |
| **Working** | "what am I doing right now?" | tau goal session | ephemeral, rebuilt each run |

- **"What's been done"** = closed tasks + merged PRs + `STATE.md`.
- **"Past mistakes"** = skills + reverted/closed-without-merge history.
- **"Overlap"** = task assignment as a claim (below).

Nothing that matters lives only in an agent's head.

## STATE.md — the company brain

A single living document at the company root summarizing: current goals, what has shipped,
what is in flight (and who owns it), key decisions, and known risks. It is small (always
fits in context) and is the agent's first read.

It stays fresh as a *side effect of working*: every run emits a `state_delta` (see
Reflection) that updates it. The **Head of Org Engineering** owns its overall health.

## Context assembly — the briefing (the most important surface)

Before each run the worker builds a **briefing** and the agent's first act is to read it —
never "start working" cold. Assembled in priority order (lower items drop first under a
token budget):

1. **Role & persona** — the agent definition. (always)
2. **`STATE.md`** — the world, big picture. (always; it's small)
3. **Active task thread** — the issue/task + its linked PR and comments. (the focus)
4. **`skills/INDEX.md`** — one line per skill; the agent pulls full skill files on demand
   when a one-liner matches the task. (always)
5. **Recent journals** for this agent — last N runs, for continuity. (best-effort)
6. **Relevant decisions** — any `mago:decision` records the task touches.

Get this right and progression works; get it wrong and nothing else matters.

## Claims — the overlap lock

Starting work on a task is a **claim**, made visible in the backend:

1. self-assign the task to the agent,
2. add an `agent:<name>` label,
3. comment a claim marker (e.g. `🔧 picking up #42`) with a timestamp.

The reconciler treats assigned-and-active tasks as **owned** and will not dispatch them
again — to another agent *or* to the same agent's next tick. Stale claims (no activity for
N) auto-release. This is a task-claim protocol for free, via the backend; it is why mago
needs no separate agent-to-agent bus.

It stops *same-task* overlap. It does **not** stop two agents editing shared code from
different tasks — that surfaces as a PR merge conflict and is resolved by the reviewer
(the CTO). Loud, not silent.

## Reflection — structural, not optional

Mistakes recur when recording them is left to the agent's good intentions. So every run
*ends* with a schema-forced structured output (tau `--schema`), and the worker — not the
agent — does the bookkeeping from it:

```json
{
  "summary":      "what I did this run",
  "state_delta":  "what changed; appended/merged into STATE.md",
  "task_status":  "in_progress | blocked | done | needs_human",
  "lessons":      [ { "skill": "<name>", "note": "learning / caveat / pitfall / gotcha" } ],
  "next":         "what should happen next run",
  "cadence_signal": "idle | working | blocked"
}
```

One structured output drives all the bookkeeping:

- `summary` + the run → the **journal** (episodic memory).
- `state_delta` → **STATE.md** update (world memory).
- `lessons[]` → append/refine **skills** + the index (lesson memory).
- `task_status` → update/close the task; release or hold the **claim**.
- `needs_human` → open/append a `mago:hitl` escalation and **pause the goal**.
- `cadence_signal` → adjust the next wake (the cheapness dial).

Because the field is *required*, a learned lesson is captured by the harness, not by hoping
the persona remembered to write it down.

## Search-before-act

A norm baked into personas and reinforced by the briefing: before starting anything, check
whether it's been tried or done — `gh issue search` / `git log` on the project repos, and
scan `skills/INDEX.md`. "Have we done this? Have we failed at this before?" is step zero.

## Retrieval at scale

Skills don't scale if you inject all of them. The pattern (the same one Claude Code's
memory uses): `skills/INDEX.md` holds one line per skill — `name — one-line hook` — and is
always in context; the full `SKILL.md` is read only when its hook matches the task. Keyword
/ description match is enough for hundreds of skills. Ranking or embeddings is a later
concern, flagged not built.

## The world backend (and why the POC is possible)

Everything above is described in GitHub terms, but "tasks / comments / claims / deliverables"
is an **interface**, not GitHub specifically:

| Capability | Production (GitHub) | Local POC |
|---|---|---|
| tasks | issues | `tasks/*.md` files |
| discussion / HITL | issue comments | a local inbox file + CLI prompt |
| deliverables | pull requests | local branches / diffs |
| claims | assignee + label | a field in the task file |
| durable state | commits (+ push) | local commits, **no push** |

The **core** — reconcile loop, context assembly, claims, reflection, skills/STATE.md — runs
identically against either backend. The POC wires the **Local** backend so the whole memory
and progression model can be smoke-tested with no GitHub, no push, no platform, no SaaS.

## Residual risks (named, not hidden)

- **STATE.md staleness** — mitigated by `state_delta` on every run; owned by Head of Org
  Engineering. Still the thing most likely to rot.
- **Skill retrieval at scale** — index+match is fine for hundreds; beyond that needs
  ranking/embeddings.
- **Cross-task code races** — claims don't cover them; PR conflict + reviewer is the catch.
- **Reflection honesty** — a `state_delta` that misreports reality poisons the world memory;
  the reconcile-from-reality rule limits the blast radius (next run re-grounds in the
  backend, not in STATE.md alone).
