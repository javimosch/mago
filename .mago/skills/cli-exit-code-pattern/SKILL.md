---
name: cli-exit-code-pattern
description: cmdWorker was the only cmd* function calling os.Exit directly. Use cliErr{code, …
---
## Notes

- 2026-06-25T07-45-10Z [cmo] cmdWorker was the only cmd* function calling os.Exit directly. Use cliErr{code, msg} to propagate semantic exit codes (80=user error per AGENTS.md) through main()'s error handler instead of os.Exit — keeps all cmd* consistent while respecting the exit-code table.
