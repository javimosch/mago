---
name: util-helpers-tests
description: util.go helpers: truncate uses byte-length + byte-slicing (s[:n]) so multibyte c…
---
## Notes

- 2026-06-23T06-53-12Z [cto] util.go helpers: truncate uses byte-length + byte-slicing (s[:n]) so multibyte chars can split — test with ASCII; orDefault preserves internal/surrounding spacing of a non-blank value (only trims for the emptiness check); readFileOr returns def for both missing AND blank-after-trim files; stripFences drops the entire first line of a fence and a trailing ``` only.
