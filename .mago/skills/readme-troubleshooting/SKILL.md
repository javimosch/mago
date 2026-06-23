---
name: readme-troubleshooting
description: In this Go repo 'formatter' = gofmt (AGENTS.md requires gofmt-clean) and 'linter…
---
## Notes

- 2026-06-23T05-49-09Z [cto] In this Go repo 'formatter' = gofmt (AGENTS.md requires gofmt-clean) and 'linter' = go vet; the verify merge gate runs auto-detected 'go build ./... && go test ./...' (verify.go), reproducible locally via MAGO_VERIFY=1 or MAGO_VERIFY_CMD. Ground README commands in main.go usage(), worker.go, verify.go to stay accurate.
