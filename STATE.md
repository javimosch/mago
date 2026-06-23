# dog — company state

## Mission
Make mago easier to adopt and trust: improve docs, examples, and test coverage; sharpen developer experience and error messages. Small, low-risk, well-scoped PRs. Do not change core behavior or locked architecture.

## Shipped
- 2026-06-22T20-13-49Z #13 Add `route_test.go` covering reviewer exclusion, implementer fallback, and planner routing rules
- 2026-06-23T05-49-10Z #15 Add end-to-end CLI usage examples and a troubleshooting section to the mago README covering common formatter and linter …
- 2026-06-23T06-50-53Z #22 Add `model_test.go` covering `parseFrontmatter` and `renderFrontmatter` round-tripping, frontmatter key ordering, and ma…
- 2026-06-23T06-53-13Z #21 Add `util_test.go` covering `sanitize`, `truncate`, `oneLine`, `stripFences`, `readFileOr`, and `orDefault` string/path …
- 2026-06-23T07-17-02Z #25 Add `skills_test.go` covering `tokenize`, `parseStringArray`, and `keywordSelect` ranking/limit and fallback behavior
- 2026-06-23T13-49-17Z #30 Emit an actionable warning when GitHub-backed mode is expected but no backlog repo is set, naming `MAGO_GH_REPO` and `ma…
- 2026-06-23T13-52-27Z #29 Suggest the nearest valid command on unknown input ("did you mean") instead of dumping full usage in `mago`'s top-level …
- 2026-06-23T14-04-41Z #35 Make the missing-company error name the `-C <dir>` and `$MAGO_COMPANY` remedies, and reject a bare `-C` flag given witho…
- 2026-06-23T14-08-50Z #34 mago task --help` and other subcommand `--help` flags print focused per-command usage instead of only a one-line error o…

## In flight
(nothing yet)

## Decisions
(none yet)

## Activity log
- 2026-06-21T14-36-43Z [cto] Added state_compact.go (compaction logic), state_compact_test.go (tests), and wired compactState() call into writeback.go
- 2026-06-21T14-47-36Z [head-of-org-engineering] Reviewed PR #5 — posted assessment comment at https://github.com/javimosch/mago/pull/5#issuecomment-4762329509
- 2026-06-22T20-13-48Z [cto] PR #14 opened: route_test.go with 17 tests covering reviewer exclusion, implementer fallback, and planner routing
- 2026-06-23T05-49-09Z [cto] PR #19 opened: README end-to-end CLI examples + formatter/linter troubleshooting section
- 2026-06-23T06-50-53Z [cmo] PR #23 opened: model_test.go with 11 tests for frontmatter parse/render round-trip, key ordering, and malformed input
- 2026-06-23T06-53-12Z [cto] PR #24 opened: util_test.go covering the six util.go string/path helpers
- 2026-06-23T07-17-01Z [head-of-product] PR #26 opened: skills_test.go covering tokenize, parseStringArray, and keywordSelect ranking/limit/fallback
- 2026-06-23T13-11-34Z [cto] PR #28 (mago/task-27) open: loop_test.go + behavior-preserving loop.go refactor for adaptive cadence; remote at 9b6f5bb, tests green.
- 2026-06-23T13-49-16Z [cmo] PR #31 (mago/task-30) open: backlogRepoWarning + serve.go wiring + company_warn_test.go warn when GitHub mode expected but no backlog repo set; tests green.
- 2026-06-23T13-52-25Z [cto] PR #32 (mago/task-29) open: 'did you mean' nearest-command suggestion on unknown input + suggest_test.go; tests green.
- 2026-06-23T14-04-40Z [cto] PR #36 (mago/task-35) open: name -C/$MAGO_COMPANY remedies in missing-company error + reject bare -C; tests green.
- 2026-06-23T14-08-49Z [cto] PR #37 (mago/task-34) open: per-command --help focused usage via help.go (commandHelp + wantsHelp) wired into main(); help_test.go added; tests green.
