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
| `mago project add <name> [--repo owner/repo]` | register a project (maps to a GitHub repo for clone→PR) |
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

- **Briefing**: role + `STATE.md` + active task (with progress log) + selected skills + recent
  journals + (for project tasks) the project's **open PRs** (so a run won't duplicate in-flight work).
- **tau**: single-shot `json+stream` subprocess, **stateless per tick** (no `--session`), with
  tools `bash,read,write,edit`, `cwd` = the task's project workspace.
- **Freshness**: for a project task the worker first fetches and checks out the task branch
  `mago/task-<id>` from the **latest `origin/<default>`** (or resumes the agent's existing remote
  branch) — work never starts from a stale clone. Large repos are cloned shallow (`--depth 1`).
- **Reflection**: requested as a trailing fenced ```json block and parsed on our side
  (we do **not** use tau `--schema` — the opencode-go provider rejects it). Schema:
  `{summary, state_delta, task_status, lessons[], next, cadence_signal, hitl_question}`.
- **Write-back** (the worker, not the agent): journal + `STATE.md` append + skills (with
  INDEX) + task status/claim + HITL + cadence.
- **Push state** (GitHub mode): agent **definitions** (`.mago/agents` + config + projects) → `main`
  (via a dedicated worktree); **runtime exhaust** (`STATE.md`, `.mago/runs|skills`) → the
  `mago-state` branch. Adopts an existing remote `mago-state`, so re-clone is fast-forward-safe.

## Project work: clone → PR → review → merge

A company maps project names to GitHub repos (`.mago/projects.json` via `project add --repo`).
For a project task:

- The **implementer** (e.g. CTO) works on the prepared branch (fresh from default), commits,
  pushes, and opens a PR — and **stops there** (it never merges its own work). A
  **deliverable guard** refuses to accept `done` on a project task unless a PR actually exists.
- The **reviewer** (`reviews: true`, e.g. Head of Org Engineering) is triggered by a
  `pull_request` webhook and reviews that PR with **no standing task**. It judges the **diff
  only** — the worker fetches `gh pr diff`, the model never gets repo access (so it can't stall
  on a huge repo), against an **explicit merge rubric** (approve if the change does what the PR
  says, is valid, is scoped, and has no bug/security/secret; request changes only for a real
  blocking defect — not for missing tests/docs/polish). On approval the worker squash-merges.
- **Dedup**: a run that finds its deliverable already shipped (a merged change or an open PR)
  sets `already_done` and the task closes with no duplicate PR.

**Verified on a real repo** (`javimosch/supercli`, ~7,400 plugins): a mago company with the
`supercli-mastery` + plugin-authoring skills built **7 plugins** as clean, isolated PRs and the
reviewer squash-merged all 7 to master — driven by GitHub webhooks through a cloudflared tunnel.

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
- State pushed to the `mago-state` branch on GitHub (defs on `main`, runtime on `mago-state`).
- **Full implement → PR → review → merge loop, webhook-driven, on a real 7,400-plugin repo** (7 plugins merged).
- Fresh-branch (work starts from the latest `origin/<default>`) + open-PR-aware dedup (`already_done`).
- Guards verified: deliverable (no over-claiming `done` without a PR), routing (reviewer bounces
  non-review tasks), and the structural reviewer judging the diff against an explicit merge rubric.

## Divergences from ARCHITECTURE (intentional, for the POC)

- **Stateless ticks**, not tau goal sessions — durable memory is the files, which made the
  progression model the thing under test (and is simpler/cheaper).
- **Reflection via prompt**, not `--schema` (provider limitation).
- **Webhook-driven** via `mago serve` (HMAC-verified `/webhook/github` → wake → reconcile in
  real time); a human comment on an agent-owned issue wakes **only that agent**, and a
  `pull_request.opened` event runs the reviewer on that PR directly (review + squash-merge,
  no standing review task — needs a webhook on the project repos). Polling
  remains a fallback. Verified end-to-end through a cloudflared tunnel: a real `issues.opened`
  event drove a routed task to `done` (issue closed). The multi-tenant relay (platform fan-out
  to NAT'd workers) is still platform-layer — a lone worker needs a public URL or a tunnel.

## Known limits / open edges

- **Cheap-model judgment drifts on subjective calls** — the reviewer needed an *explicit* rubric
  (and the implementer/reviewer needed structural "don't browse the repo" guards) because prompt
  hints alone aren't obeyed. The merge *mechanics* are solid; the *criteria* must be pinned down.
- Same GitHub account → the reviewer merges **without a formal GitHub approval** (a GitHub App /
  per-agent identity would fix this and the self-approve gap).
- First tick right after a fresh clone has its exhaust reset by the `mago-state` adopt.
- Transient model API errors (exit 110) are handled by re-triggering the affected PR/tick.

## Not built yet

Platform backend, accounts (email/password), Stripe, the two-binary split, the multi-tenant
**webhook relay** (platform fan-out to NAT'd workers), GitHub App / per-agent identity, and the
`decisions/` mechanism.

Note: many earlier "not built" items are now done — exec-team personas, STATE.md compaction,
real project clone→branch→PR→review→merge, git worktrees, agent-side dedup (`already_done`), the
PR→reviewer trigger, fresh-branch/open-PR awareness, and the structural reviewer rubric.
