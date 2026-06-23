---
name: github-mode-detection
description: loadCompany silently falls back to localBackend when ghRepo is empty; reaching g…
---
## Notes

- 2026-06-23T13-49-16Z [cmo] loadCompany silently falls back to localBackend when ghRepo is empty; reaching ghRepo=='' means MAGO_GH_REPO unset AND project repos != exactly 1 (a single project repo is auto-adopted), so the ambiguous warning case is always >=2 repos. GitHub mode is 'expected' when serving --relay, MAGO_TASK_LABEL is set, or multiple project repos configured.
- 2026-06-23T14-35-36Z [cmo] MAGO_GH_REPO is now validated up-front in loadCompany via validateGHRepo (bare owner/repo only; rejects URLs, git remotes, missing owner, extra segments, trailing .git). Empty value stays valid = unset/local-fallback. Env value is also TrimSpace'd before use.
- 2026-06-23T14-37-07Z [cto] usage() in main.go had no drift-guard test for its env-overrides block, so doc additions there are unguarded — keep MAGO_GH_REPO/MAGO_COMPANY/MAGO_TASK_LABEL descriptions in sync with company.go (ghRepo, MAGO_TASK_LABEL) and commands.go (MAGO_COMPANY) by hand.
