---
name: command-dispatch-suggestions
description: main.go dispatch is a flat switch on os.Args[1]; the canonical command list now …
---
## Notes

- 2026-06-23T13-52-25Z [cto] main.go dispatch is a flat switch on os.Args[1]; the canonical command list now lives in knownCommands (suggest.go) and must be kept in sync with that switch — TestKnownCommandsSuggestThemselves guards against drift. suggestCommand uses a length-aware threshold (<=2 edits, <= half input len) plus prefix matching so unrelated input yields no misleading suggestion.
- 2026-06-23T14-04-40Z [cto] parseCompanyDir in commands.go now returns (string, []string, error) and rejects a bare/empty -C; all 12 cmdX callers propagate the error. The missing-.mago error in loadCompany now points at -C and $MAGO_COMPANY, not just `mago init`.
- 2026-06-23T14-08-49Z [cto] Per-command help lives in commandHelp (help.go), keyed by the same names as main()'s dispatch switch; help_test.go's dispatchCommands list guards drift (knownCommands from suggest.go/PR #32 is not on this branch yet, so the test is self-contained). main() intercepts -h/--help BEFORE the switch, so commands never see help as a positional.
- 2026-06-23T14-21-01Z [cto] suggest.go (knownCommands/levenshtein/suggestCommand from PR #32) is NOT yet on master, so task-39's branch can't reuse it — used a self-contained nearestAction/actionDistance with distinct names to keep the branch compiling standalone AND avoid a duplicate-symbol merge collision if #32 lands first. Sub-action lists (projectActions in commands.go, workerActions in worker.go) live next to their dispatch switches with drift-guard tests. Only suggest on genuinely-unknown actions; a valid action missing args (e.g. `project add` w/o name) still gets the usage block.
