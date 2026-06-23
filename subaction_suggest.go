package main

import "strings"

// nearestAction returns the closest valid sub-action to input, or "" when
// nothing is close enough to make a useful "did you mean" suggestion. It mirrors
// the behaviour of the top-level command suggester: prefix matches are treated
// as a strong signal, and otherwise a length-aware edit-distance threshold (at
// most 2 edits, never more than half the input length) keeps genuinely
// unrelated input from surfacing a misleading suggestion.
//
// It is intentionally self-contained (its own distance helper) so each command
// group can suggest over its own small, canonical sub-action list without
// depending on the top-level command suggester.
func nearestAction(input string, valid []string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return ""
	}
	maxDist := 2
	if half := len(input) / 2; half < maxDist {
		maxDist = half
	}
	if maxDist < 1 {
		maxDist = 1
	}
	best, bestDist := "", maxDist+1
	for _, v := range valid {
		if strings.HasPrefix(v, input) || strings.HasPrefix(input, v) {
			return v
		}
		if d := actionDistance(input, v); d < bestDist {
			best, bestDist = v, d
		}
	}
	if bestDist <= maxDist {
		return best
	}
	return ""
}

// actionDistance is the Levenshtein edit distance between a and b (insertions,
// deletions, substitutions each cost 1), computed over runes so multibyte input
// is handled correctly.
func actionDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
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
			cur[j] = minOf3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}

func minOf3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
