package main

import (
	"fmt"
	"sort"
	"strings"
)

// agentNameDistance is the Levenshtein edit distance between a and b, compared
// rune-wise so multibyte agent names behave sensibly. Distinct from the command
// and sub-action distance helpers so this stays self-contained.
func agentNameDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
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

// nearestAgentName returns the candidate closest to input, or "" when nothing is
// close enough to suggest. A case-insensitive prefix match wins outright;
// otherwise the best candidate must be within a length-aware threshold (at most
// 2 edits and at most half the input's length) so unrelated typos don't yield a
// misleading suggestion.
func nearestAgentName(input string, candidates []string) string {
	in := strings.ToLower(strings.TrimSpace(input))
	if in == "" {
		return ""
	}
	// Prefix match first — "ct" -> "cto".
	var prefix []string
	for _, c := range candidates {
		if strings.HasPrefix(strings.ToLower(c), in) {
			prefix = append(prefix, c)
		}
	}
	if len(prefix) == 1 {
		return prefix[0]
	}

	best, bestDist := "", -1
	for _, c := range candidates {
		d := agentNameDistance(in, strings.ToLower(c))
		if bestDist == -1 || d < bestDist {
			best, bestDist = c, d
		}
	}
	threshold := 2
	if half := len([]rune(in)) / 2; half < threshold {
		threshold = half
	}
	if bestDist >= 0 && bestDist <= threshold {
		return best
	}
	return ""
}

// unknownAgentError builds the "agent not found" error for `mago run`/`mago loop`,
// keeping the agent name and directory (callers/tests rely on both) while adding a
// nearest-match suggestion and the list of available agents.
func unknownAgentError(name, agentsDir string, candidates []string) error {
	base := fmt.Sprintf("agent %q not found in %s", name, agentsDir)
	if len(candidates) == 0 {
		return fmt.Errorf("%s\n  no agents are defined yet — run `mago init` to scaffold them", base)
	}
	sorted := append([]string(nil), candidates...)
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString(base)
	if s := nearestAgentName(name, candidates); s != "" {
		fmt.Fprintf(&b, "\n  did you mean %q?", s)
	}
	fmt.Fprintf(&b, "\n  available agents: %s", strings.Join(sorted, ", "))
	return fmt.Errorf("%s", b.String())
}
