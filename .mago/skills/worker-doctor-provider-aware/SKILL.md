---
name: worker-doctor-provider-aware
description: checkClaudeAuth runs a real `claude -p ping --output-format json` probe — safe…
---
## Notes

- 2026-06-23T22-56-51Z [cto] checkClaudeAuth runs a real `claude -p ping --output-format json` probe — safe because unauthenticated claude returns fast (no LLM call), but when claude IS on PATH and logged in it will make a tiny API call. For unit tests, avoid testing checkClaudeAuth directly; instead expose a pure providerCheckNames() selection function and test that. The transientClaude() helper (claude.go) is reusable from worker.go since they're in the same package.
