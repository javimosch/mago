package main

import "testing"

func TestActionDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"add", "add", 0},
		{"", "add", 3},
		{"list", "", 4},
		{"lst", "list", 1},     // one insertion
		{"adde", "add", 1},     // one deletion
		{"dctor", "doctor", 1}, // one insertion
		{"moed", "mode", 2},    // transposition = two substitutions
	}
	for _, c := range cases {
		if got := actionDistance(c.a, c.b); got != c.want {
			t.Errorf("actionDistance(%q,%q)=%d, want %d", c.a, c.b, got, c.want)
		}
		// distance is symmetric
		if got := actionDistance(c.b, c.a); got != c.want {
			t.Errorf("actionDistance(%q,%q)=%d, want %d (symmetry)", c.b, c.a, got, c.want)
		}
	}
}

func TestActionDistanceMultibyte(t *testing.T) {
	// runes, not bytes: "é" -> "e" is a single substitution
	if got := actionDistance("modé", "mode"); got != 1 {
		t.Errorf("actionDistance multibyte = %d, want 1", got)
	}
}

func TestNearestActionProject(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"lst", "list"},    // typo (deletion)
		{"liste", "list"},  // prefix of input -> list
		{"li", "list"},     // prefix of valid -> list
		{"adde", "add"},    // typo
		{"ADD", "add"},     // case-insensitive
		{"  add  ", "add"}, // trimmed
		{"remove", ""},     // unrelated -> no suggestion
		{"", ""},           // empty -> no suggestion
		{"xyzzy", ""},      // far -> no suggestion
	}
	for _, c := range cases {
		if got := nearestAction(c.in, projectActions); got != c.want {
			t.Errorf("nearestAction(%q, projectActions)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestNearestActionWorker(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"dctor", "doctor"}, // typo (deletion)
		{"doc", "doctor"},   // prefix of valid
		{"moed", "mode"},    // transposition (2 edits) within threshold
		{"status", ""},      // unrelated -> no suggestion
		{"", ""},
	}
	for _, c := range cases {
		if got := nearestAction(c.in, workerActions); got != c.want {
			t.Errorf("nearestAction(%q, workerActions)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestNearestActionTask(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"aad", "add"}, // typo (transposition)
		{"ad", "add"},  // prefix of valid
		{"ADD", "add"}, // case-insensitive
		{"list", ""},   // unrelated -> no suggestion
		{"remove", ""}, // unrelated -> no suggestion
		{"", ""},
	}
	for _, c := range cases {
		if got := nearestAction(c.in, taskActions); got != c.want {
			t.Errorf("nearestAction(%q, taskActions)=%q, want %q", c.in, got, c.want)
		}
	}
}

// Drift guards: every canonical action must suggest itself, so the lists used
// for "did you mean" stay aligned with the dispatch switches.
func TestProjectActionsSuggestThemselves(t *testing.T) {
	for _, a := range projectActions {
		if got := nearestAction(a, projectActions); got != a {
			t.Errorf("project action %q suggested %q", a, got)
		}
	}
}

func TestWorkerActionsSuggestThemselves(t *testing.T) {
	for _, a := range workerActions {
		if got := nearestAction(a, workerActions); got != a {
			t.Errorf("worker action %q suggested %q", a, got)
		}
	}
}

func TestTaskActionsSuggestThemselves(t *testing.T) {
	for _, a := range taskActions {
		if got := nearestAction(a, taskActions); got != a {
			t.Errorf("task action %q suggested %q", a, got)
		}
	}
}
