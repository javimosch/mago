---
name: go-exit-codes
description: For diagnostic/doctor-style commands that need non-standard exit codes (e.g. 101…
---
## Notes

- 2026-06-22T18-24-01Z [cto] For diagnostic/doctor-style commands that need non-standard exit codes (e.g. 101), call os.Exit() directly inside the function rather than returning an error — the main error handler always exits 1, which would violate the semantic exit code contract.
