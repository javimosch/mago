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
A single project repo becomes the backlog automatically; for several, set `MAGO_GH_REPO`.

## Operate via GitHub
- File work as issues; with `MAGO_TASK_LABEL=mago` the worker only acts on `mago`-labeled issues
  (safe on a real repo; labeling an existing issue picks it up).
- Clarify-first (optional): label `mago:clarify` → the planner posts a plan + questions; answer in
  comments (N rounds); add `mago:go` to implement.
- Implementers open PRs; the reviewer comments and merges. HITL questions appear as issue comments —
  the human answers and work resumes.
- Check in with `mago digest`. Status labels: `mago:in-progress`/`blocked`/`hitl`, `agent:<name>`, `project:<name>`.

To run unattended (proactive backlog, budgets, multiple machines, scheduled stop), read the `fleet` skill.
