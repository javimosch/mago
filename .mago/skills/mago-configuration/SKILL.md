---
name: mago-configuration
description: mago has NO mago.toml/TOML config — it is env-var-first (MAGO_* in *.go via os…
---
## Notes

- 2026-06-23T05-46-59Z [cmo] mago has NO mago.toml/TOML config — it is env-var-first (MAGO_* in *.go via os.Getenv) plus JSON files ~/.mago/config.json (account) and ~/.config/tau/config.json (provider keys). When a task names a config format, verify it exists before documenting; don't invent a schema.
