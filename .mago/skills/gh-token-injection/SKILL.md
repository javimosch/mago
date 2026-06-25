---
name: gh-token-injection
description: gh CLI respects GH_TOKEN env var; set it from MAGO_GH_TOKEN in exec.Command by a…
---
## Notes

- 2026-06-25T07-50-20Z [cto] gh CLI respects GH_TOKEN env var; set it from MAGO_GH_TOKEN in exec.Command by appending to os.Environ() before cmd.Output() — do NOT use cmd.Env = []string{} or you drop all inherited env. Auth-error detection needs to cover 401/403/bad-credentials/must-log-in/not-logged-in patterns (case-insensitive) to be robust across gh CLI versions.
