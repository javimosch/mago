---
name: github-mode-detection
description: loadCompany silently falls back to localBackend when ghRepo is empty; reaching g…
---
## Notes

- 2026-06-23T13-49-16Z [cmo] loadCompany silently falls back to localBackend when ghRepo is empty; reaching ghRepo=='' means MAGO_GH_REPO unset AND project repos != exactly 1 (a single project repo is auto-adopted), so the ambiguous warning case is always >=2 repos. GitHub mode is 'expected' when serving --relay, MAGO_TASK_LABEL is set, or multiple project repos configured.
