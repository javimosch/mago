---
name: routing-tests-llm-fallback
description: routeTask's LLM call (tauComplete) retries 3x with 2s+4s backoff on failure. In …
---
## Notes

- 2026-06-22T20-13-48Z [cto] routeTask's LLM call (tauComplete) retries 3x with 2s+4s backoff on failure. In CI without an API key this totals ~6s per test call but always falls to the deterministic fallback path — making it testable without mocking. Keep routeTask test count small (4) to bound total wall time (~24s).
