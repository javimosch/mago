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

Platform backend (ours) ── accounts + Stripe + webhook relay
   GitHub webhook ──▶ platform ──▶ your worker (dials out, NAT-friendly)
```

## Status

Pre-implementation. Design is locked; see [`docs/`](docs/):

- [VISION.md](docs/VISION.md) — what mago is and the bet behind it
- [ARCHITECTURE.md](docs/ARCHITECTURE.md) — the layers, binaries, data flow, git-native model
- [ROADMAP.md](docs/ROADMAP.md) — v1 scope and what waits for v2
- [AGENTS.md](AGENTS.md) — coding guidelines for working in this repo

## Pricing

Single plan, €20/month. BYOK — your LLM provider bills you for tokens directly.
