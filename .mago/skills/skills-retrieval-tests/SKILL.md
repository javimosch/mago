---
name: skills-retrieval-tests
description: skills.go helpers are pure: tokenize keeps only >=4-rune lowercased [a-z0-9] tok…
---
## Notes

- 2026-06-23T07-17-01Z [head-of-product] skills.go helpers are pure: tokenize keeps only >=4-rune lowercased [a-z0-9] tokens (so 'api'/'the' drop) and dedups via a map; parseStringArray returns nil (not empty) on no-brackets/malformed/non-string-elements but a non-nil empty slice for '[]'; keywordSelect ranks by overlapping-token count desc and returns nil when nothing overlaps. No mocking needed — keyword fallback path is fully deterministic, unlike the LLM selector.
