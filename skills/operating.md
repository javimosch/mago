---
name: operating
description: operate mago for a human CEO — harness, onboard, run a company, ship PRs
---

# Operating mago

You are an AI agent operating mago on behalf of a human (the CEO). Drive everything via the `mago`
and `gh` CLIs. The human only: pays, installs the GitHub App, answers clarify/HITL questions, says "go".

## Harness (BYOK — pick one)
- **Claude Code:** `claude` on PATH + logged in; run agents with `MAGO_PROVIDER=claude MAGO_MODEL=sonnet`
  (or `opus`). No API key. If the worker runs under a custom HOME, set `CLAUDE_CONFIG_DIR=~/.claude`.
- **tau** (https://github.com/javimosch/tau): on PATH, key in `~/.config/tau/config.json`
  (`{"keys":{"opencode-go":"sk-..."}}`, chmod 600) or `OPENCODE_API_KEY`.

Also: `gh` authenticated (`gh auth status`); run `gh auth setup-git` once so the worker can push to
private repos. mago resells no completions — your harness, your key/subscription.

## Onboard
```
mago register --email you@co.com --password <pw>   # account + 48h no-card trial; license cached
mago account status                                 # plan, trial window, license
mago subscribe                                       # €20/mo Stripe link (the human pays)
mago billing                                         # manage/cancel (Stripe portal)
mago link --installation <id>                        # entitle your repos (id from the App install URL)
```

## Run a company
```
mago init ./company
mago project add <name> --repo owner/repo
MAGO_TASK_LABEL=mago mago serve --relay -C ./company    # worker dials out; agents wake on GitHub events
```

**Two layouts:**
- **Single repo** (default): point the worker at one repo — its issues are the backlog, agents work
  that repo, and PRs close its issues directly. `mago project add owner/repo` (a lone project repo is
  auto-adopted as the backlog) or set `MAGO_GH_REPO=owner/repo`.
- **Multi-project** (one worker, several repos): one **backlog repo** (`MAGO_GH_REPO`) is the command
  center where you file issues; register the others with `mago project add <name> --repo owner/repo`.
  An issue labeled `project:<name>` (file it with `mago task add "<title>" --project <name>`) has its
  PR opened on **that project's** repo. The worker watches all of them over one relay connection.
  ⚠️ Proactive planning only files into the **backlog** repo — so multi-project is for **reactive**
  dispatch; keep **proactive** single-repo (one worker per repo). See the `fleet` skill.

## Set direction (so agents work on what matters, not filler)
`mago init` scaffolds two steering files in the company dir — edit them so autonomous work advances
the product instead of producing busywork:
- **VISION.md** — stable: `## North star`, `## Product`, `## Constraints` (no-touch / locked areas).
  Agents read it for intent; they never rewrite it.
- **ROADMAP.md** — steerable: `## Now` (the current focus), `## Next`, `## Later`, `## Out of scope`.
The planner proposes only work that advances `## Now` (and may propose nothing if there's no valuable
in-scope work — no filler); the implementer brief carries the focus + no-touch; the reviewer rejects
out-of-scope / no-touch edits. If ROADMAP.md is absent the focus falls back to STATE.md `## Mission`.

**Outcome loop (auto-advance):** when the focus is achieved (work shipped, backlog drained) the planner
signals completion and mago rotates the roadmap on its own — archives `## Now` into `## Done`
(timestamped), then `Now ← Next ← Later`. So you set `Next`/`Later` once and the company advances
through them without you editing `Now` each time. It won't advance while work is in flight, and holds
(asking you to set `Next`) if there's nothing queued.

## Operate via GitHub
- File work as issues; with `MAGO_TASK_LABEL=mago` the worker only acts on `mago`-labeled issues
  (safe on a real repo; labeling an existing issue picks it up).
- Clarify-first (optional): label `mago:clarify` → the planner posts a plan + questions; answer in
  comments (N rounds); add `mago:go` to implement.
- Implementers open PRs; the reviewer comments and merges. HITL questions appear as issue comments —
  the human answers and work resumes.
- Check in with `mago digest`. Status labels: `mago:in-progress`/`blocked`/`hitl`, `agent:<name>`, `project:<name>`.

Re-aim a worker on the fly — reactive ↔ proactive, review ↔ verified autonomy, comms on/off — without
restarting it: `mago mode <tokens> -C <dir>` locally, or `mago worker mode <tokens> --worker <id>|--all`
over the relay. See the `fleet` skill ("Runtime mode").

## Tell the mago team what isn't working
Hit a confusing error, a blocking point, or want a feature? File it from the CLI:
```
mago feedback "<what didn't click>" [--type bug|friction|feature|question]
```
It reaches the mago team as a triage item. **As the operating agent, self-report:** whenever a mago
command fails, behaves unclearly, or blocks you, run `mago feedback` describing exactly what happened —
that friction is the most valuable signal for improving mago.

To run unattended (proactive backlog, budgets, multiple machines, scheduled stop), read the `fleet` skill.
