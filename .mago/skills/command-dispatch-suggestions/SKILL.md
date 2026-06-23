---
name: command-dispatch-suggestions
description: main.go dispatch is a flat switch on os.Args[1]; the canonical command list now …
---
## Notes

- 2026-06-23T13-52-25Z [cto] main.go dispatch is a flat switch on os.Args[1]; the canonical command list now lives in knownCommands (suggest.go) and must be kept in sync with that switch — TestKnownCommandsSuggestThemselves guards against drift. suggestCommand uses a length-aware threshold (<=2 edits, <= half input len) plus prefix matching so unrelated input yields no misleading suggestion.
