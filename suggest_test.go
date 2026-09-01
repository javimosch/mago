package main

import "testing"

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
		{"statuss", "status", 1},
		{"tsak", "task", 2},
		{"café", "cafe", 1}, // multibyte rune handled correctly
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		// distance is symmetric
		if got := levenshtein(c.b, c.a); got != c.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d (symmetry)", c.b, c.a, got, c.want)
		}
	}
}

func TestSuggestCommand(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// single-edit typos resolve to the intended command
		{"statuss", "status"},
		{"taks", "task"},
		{"serv", "serve"},
		{"versionn", "version"},
		{"logni", "login"},
		// prefixes of a known command
		{"ini", "init"},
		{"dig", "digest"},
		// case-insensitive
		{"TASK", "task"},
		{"Status", "status"},
		// surrounding whitespace tolerated
		{"  task ", "task"},
		// nothing close enough -> no suggestion
		{"completely-unrelated", ""},
		{"xyz", ""},
		{"", ""},
		// single short character that is not a prefix of any known command
		{"x", ""},
		{"ab", ""},
		// exact command still maps to itself (harmless; dispatch handles real ones)
		{"task", "task"},
	}
	for _, c := range cases {
		if got := suggestCommand(c.in); got != c.want {
			t.Errorf("suggestCommand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Every command reachable in main()'s dispatch switch should be representable
// as a suggestion, guarding against the list drifting out of sync.
func TestKnownCommandsSuggestThemselves(t *testing.T) {
	for _, cmd := range knownCommands {
		if got := suggestCommand(cmd); got != cmd {
			t.Errorf("suggestCommand(%q) = %q, want exact match", cmd, got)
		}
	}
}
