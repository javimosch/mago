# Vision

## North star

**Cheap autonomous agents that run companies.**

## The bet

Autonomy is a ladder — chatbots that only talk, copilots that act with a human driving,
and **agents** that run on their own. The agent tier is the hard, valuable one, and most
platforms treat it as a far-off destination. mago is *only* that tier, made cheap enough
to leave running.

A mago **agent** is a long-lived worker, not a chat session: it has an identity, a
workspace, a trigger (manual, cron, or auto-adjusted cadence), channels it listens on,
and memory that survives across runs. A set of agents over a GitHub repo is a **company**,
and the human is its **CEO**. mago seeds an executive team — a **CTO**, a **CMO**, a
**Head of Product**, and a **Head of Org Engineering** — reporting to the CEO. The CEO sets
direction by filing issues and answering the occasional question; the team does the work
across the company's project repos and lands it as pull requests.

## Principles

1. **Agents, not assistants.** mago does one thing — autonomy-level-3 agents — and does
   it well. No chatbot/copilot ladder, no "assistant" framing.
2. **The repo is the company.** State, tasks, discussion, and audit trail all live in a
   GitHub repo the client owns. Issues are the backlog, PRs are deliverables, comments are
   the conversation. `git clone` is the backup and the audit log.
3. **Cheap by construction.** Agents back off when idle (adaptive cadence), default to
   cheap models, and run under token budgets. Idle companies cost almost nothing.
4. **BYOK — we never provide completions.** The client's LLM key stays on their worker
   and pays their provider directly. mago sells orchestration, not tokens.
5. **The client owns everything.** Data lives in the client's GitHub; compute runs on the
   client's worker. mago holds an account and a relay, nothing more.
6. **Agent-first surface.** The CLI is built to be driven by an agent (JSON out, semantic
   exit codes, scriptable). The human's own agent does the onboarding.
7. **Transparent by default.** All coordination happens as GitHub issues/PRs/comments the
   human can read. Visibility is the trust, and the trust is the product.
8. **Skills are memory; the company learns.** Agents read shared skills before acting and
   improve them after — capturing learnings, caveats, pitfalls, and gotchas. A mago company
   gets cheaper and better at its job over time without the CEO curating every lesson.

## What "runs a company" means

The CEO opens an issue: "we need CSV export in the reports product." The Head of Product
turns it into a spec and prioritizes it. The CTO picks it up in a worktree on the relevant
project repo, reads the relevant skills, does the work, and opens a PR — pausing to ask the
CEO only on a real decision (which CSV dialect?). The PR is reviewed and merged; the CTO
refines a skill with what it learned. When nothing is pending, every agent decays to its
slowest cadence and the company costs almost nothing until the next issue. The CEO touched
it twice: filing the issue and answering one question.

## Non-goals (for now)

- Selling or proxying LLM completions.
- A web panel or dashboard — mago is CLI-only.
- Chatbot or copilot tiers.
- Being a vertical tool. mago is generic orchestration; the company's purpose is whatever
  the client points it at.
