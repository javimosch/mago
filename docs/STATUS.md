# Status — what's actually built

This tracks the working POC versus the designed system in
[ARCHITECTURE.md](ARCHITECTURE.md). The POC validates the **core loop** end to end on
real models, with no platform, SaaS, accounts, or billing.

## The binary

A single Go binary `mago` (the client+worker bundled; the operator/platform binary is
not built yet). Commands:

| Command | What it does |
|---|---|
| `mago init [dir]` | scaffold a company: `.mago/` (agents, skills, runs, inbox), `STATE.md`, `tasks/`, `workspace/`, `projects/` |
| `mago task add "<title>" [--project p]` | create a task (local file or GitHub issue) |
| `mago project add <name>` | register a project repo/workspace |
| `mago run <agent>` | run ONE tick for one agent |
| `mago tick` | reconcile once: route open tasks to best-fit agents, then run each |
| `mago loop [<agent>]` | run ticks on an adaptive cadence (no agent = loop the full reconcile) |
| `mago status` | show STATE.md, tasks, pending HITL |
| `mago answer <id> "<text>"` | answer a needs-human task so it resumes (local mode) |
| `mago serve` | event-driven worker: GitHub webhooks wake a reconcile in real time (`--addr`, `--secret`, `--heartbeat`) |

Env knobs (BYOK — keys stay on this machine, used by tau):
`MAGO_PROVIDER` / `MAGO_MODEL` (override the agent's tau provider/model),
`MAGO_GH_REPO=owner/repo` (switch the task backend to GitHub), `-C <dir>` / `$MAGO_COMPANY`
(company directory). Requires `tau` and `gh` on `PATH`.

## The tick (as built)

```
pick task (claim) -> assemble briefing -> drive tau -> parse reflection -> write back -> push state
```

- **Briefing**: role + `STATE.md` + active task (with progress log) + selected skills + recent journals.
- **tau**: single-shot `json+stream` subprocess, **stateless per tick** (no `--session`), with
  tools `bash,read,write,edit`, `cwd` = the task's project workspace.
- **Reflection**: requested as a trailing fenced ```json block and parsed on our side
  (we do **not** use tau `--schema` — the opencode-go provider rejects it). Schema:
  `{summary, state_delta, task_status, lessons[], next, cadence_signal, hitl_question}`.
- **Write-back** (the worker, not the agent): journal + `STATE.md` append + skills (with
  INDEX) + task status/claim + HITL + cadence.
- **Push state** (GitHub mode): agent **definitions** (`.mago/agents` + config + projects) → `main`
  (via a dedicated worktree); **runtime exhaust** (`STATE.md`, `.mago/runs|skills`) → the
  `mago-state` branch. Adopts an existing remote `mago-state`, so re-clone is fast-forward-safe.

## Backends (the world)

Selected by `MAGO_GH_REPO`. Behind a `TaskBackend` interface:

- **Local** (default): tasks are `tasks/*.md`, HITL is an inbox file + `mago answer`.
- **GitHub**: tasks are issues; status is labels (`mago:in-progress` / `mago:blocked` /
  `mago:hitl` + `agent:<name>` + `project:<name>`); progress/HITL are comments; `done`
  closes the issue. A human comment on a `mago:hitl` issue is detected on poll and flips
  it back to in-progress so the agent resumes (production would use a webhook).

## Memory (as built)

File-based, matching [MEMORY.md](MEMORY.md): world = `STATE.md`; task = the task/issue +
its progress log; lessons = `.mago/skills/<name>/SKILL.md` with an always-in-context
`INDEX.md` and an LLM selector that injects only relevant full skills; episodic =
`.mago/runs/<agent>/`. Each tick re-grounds from these files (the session is not trusted).

## Verified end to end (real models, opencode-go/deepseek-v4-flash)

- No-redo, resume across stateless ticks, build-on-prior-work.
- Claims / no-overlap; reviewer refuses another agent's in-progress task.
- HITL — local (`mago answer`) and **GitHub** (human comments on the issue → resume).
- Learn-from-self (reviewer findings → skills) and **skill-driven pitfall avoidance**,
  including retrieval among 20 noise skills.
- Adaptive cadence backoff; self-healing on unparseable ticks.
- Role→task routing; one company driving N project repos.
- State pushed to the `mago-state` branch on GitHub.

## Divergences from ARCHITECTURE (intentional, for the POC)

- **Stateless ticks**, not tau goal sessions — durable memory is the files, which made the
  progression model the thing under test (and is simpler/cheaper).
- **Reflection via prompt**, not `--schema` (provider limitation).
- **Per-project workspace dirs**, not git worktrees, and **no real project clones/PRs** yet
  — agents work in local dirs; product code is not pushed.
- **Webhook-driven** via `mago serve` (HMAC-verified `/webhook/github` → wake → reconcile in
  real time); a human comment on an agent-owned issue wakes **only that agent**, and a
  `pull_request.opened` event runs the reviewer on that PR directly (review + squash-merge,
  no standing review task — needs a webhook on the project repos). Polling
  remains a fallback. Verified end-to-end through a cloudflared tunnel: a real `issues.opened`
  event drove a routed task to `done` (issue closed). The multi-tenant relay (platform fan-out
  to NAT'd workers) is still platform-layer — a lone worker needs a public URL or a tunnel.
- Starter agents in tests are a generic `cto` / `reviewer`, not the full exec team.

## Not built yet

Platform backend, accounts (email/password), Stripe, the two-binary split, the webhook
relay, GitHub App, the `decisions/` mechanism, agent-side **dedup** of already-shipped work,
and a recurring **review trigger** for a continuous PR stream (today's review is a one-off task).

Note: several earlier "not built" items are now done — exec-team personas, STATE.md
compaction, real project clone→branch→PR, and git worktrees (used for the def/runtime split).
