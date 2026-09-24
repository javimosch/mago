package main

import "strings"

// knownCommands is the canonical list of top-level commands, used both for the
// unknown-command "did you mean" suggestion and to keep that list in one place.
// Keep in sync with the dispatch switch in main().
var knownCommands = []string{
	"init", "task", "project", "run", "loop", "tick", "serve", "daemon", "status",
	"answer", "digest", "skills", "mode", "feedback", "register", "login",
	"subscribe", "billing", "account", "link", "worker", "update", "install",
	"uninstall", "claim", "version", "help", "help-json", "guide",
}

// levenshtein returns the edit distance between a and b (insertions, deletions,
// substitutions each cost 1). Operates on runes so multibyte input is handled.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	// prev/cur are rows of the DP matrix; only two rows are needed.
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// suggestCommand returns the closest known command to the given input, or ""
// when nothing is close enough to be a useful suggestion. A command is only
// offered when its edit distance is within a small, length-aware threshold so
// that genuinely unrelated input (e.g. a typo for a different word) does not
// surface a misleading "did you mean".
func suggestCommand(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return ""
	}
	// An exact command always suggests itself. Without this, the prefix rule below
	// returns whichever related command comes first in the list, so a real command
	// that extends another one ("help-json" vs "help") gets "corrected" to the
	// shorter one.
	for _, cmd := range knownCommands {
		if cmd == input {
			return cmd
		}
	}
	// Allow at most 2 edits, and never more than half the input length, so very
	// short inputs require a near-exact match.
	maxDist := 2
	if half := len(input) / 2; half < maxDist {
		maxDist = half
	}
	if maxDist < 1 {
		maxDist = 1
	}
	// An exact match always wins — otherwise a shorter command that is a prefix
	// of the input would shadow it in the loop below (e.g. "help-json" -> "help").
	for _, cmd := range knownCommands {
		if cmd == input {
			return cmd
		}
	}
	best, bestDist := "", maxDist+1
	for _, cmd := range knownCommands {
		// A known command that the input is a prefix of (or vice versa) is a
		// strong signal regardless of raw distance (e.g. "ini" -> "init").
		if strings.HasPrefix(cmd, input) || strings.HasPrefix(input, cmd) {
			return cmd
		}
		if d := levenshtein(input, cmd); d < bestDist {
			best, bestDist = cmd, d
		}
	}
	if bestDist <= maxDist {
		return best
	}
	return ""
}
