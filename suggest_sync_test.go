package main

import (
	"os"
	"regexp"
	"testing"
)

// TestKnownCommandsCoverDispatch guards the #32 invariant: every top-level command in main()'s
// dispatch switch must be in knownCommands (the did-you-mean / canonical list). Catches the drift
// where a new command (e.g. feedback) is wired into dispatch but forgotten in knownCommands.
func TestKnownCommandsCoverDispatch(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	known := map[string]bool{}
	for _, c := range knownCommands {
		known[c] = true
	}
	// dispatch cases look like: case "init": / case "version", "-v", "--version":
	re := regexp.MustCompile(`case "([a-z][a-z-]*)"`)
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		cmd := m[1]
		if !known[cmd] {
			t.Errorf("command %q is dispatched in main.go but missing from knownCommands (suggest.go)", cmd)
		}
	}
}
