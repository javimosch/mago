# mago skills — how mago works

Reference knowledge for agents working **with** mago (operating it, testing it, extending it).
For **coding conventions** in this repo, see [`../../AGENTS.md`](../../AGENTS.md). For design
rationale, see [`../../docs/`](../../docs/).

| Skill | Read it when you need to… |
|---|---|
| [core-vs-platform.md](core-vs-platform.md) | understand the core/platform split, what lives where, and the stdlib-only-core rule |
| [agent-runtime.md](agent-runtime.md) | understand how the worker + agent team actually run (ticks, tau, GitHub-native state, relay, routing, review) |
| [cli-usage.md](cli-usage.md) | drive the `mago` CLI (commands, config, env vars) |
| [live-testing.md](live-testing.md) | run things for real: build, operator simulation, the live ship-code capstone |

**One-line mental model:** mago is a CLI platform where the operator (CEO) files GitHub issues
and an autonomous AI agent team (CTO/CMO/Head of Product/Head of Org Engineering) ships PRs to
their repos. The operator runs a **worker** (BYOK, on their machine). Optionally, a hosted
**platform** (separate private repo) handles billing + licence + relaying GitHub webhooks to a
NAT'd worker — everything here works without it.
