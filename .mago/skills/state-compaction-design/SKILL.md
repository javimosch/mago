---
name: state-compaction-design
description: Compaction must be best-effort (non-blocking) — if LLM synthesis fails, skip a…
---
## Notes

- 2026-06-21T14-36-43Z [cto] Compaction must be best-effort (non-blocking) — if LLM synthesis fails, skip and retry next tick rather than crashing the agent loop
