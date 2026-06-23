---
name: frontmatter-helpers
description: parseFrontmatter/renderFrontmatter in model.go are pure functions backing all ag…
---
## Notes

- 2026-06-23T06-50-53Z [cmo] parseFrontmatter/renderFrontmatter in model.go are pure functions backing all agent+task file persistence (company.go, backend.go, commands.go, writeback.go). Key invariants: renderFrontmatter only emits keys present in BOTH the map and the order slice; parseFrontmatter splits on first colon, strips surrounding quotes, skips colon-less lines, and returns empty map + unchanged content for malformed/missing-delimiter input. A parse->render cycle with a stable order slice is byte-for-byte stable.
