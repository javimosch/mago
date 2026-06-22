---
name: gh-auth-check
description: `gh auth status` exits 0 when authenticated and non-zero otherwise; suppress std…
---
## Notes

- 2026-06-22T18-24-01Z [cto] `gh auth status` exits 0 when authenticated and non-zero otherwise; suppress stdout/stderr with cmd.Stdout = nil / cmd.Stderr = nil to keep doctor output clean.
