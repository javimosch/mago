package main

import (
	"strings"
	"testing"
)

// TestUsage verifies the global usage banner prints the version and references
// the main command groups, so it stays in sync with the binary's surface.
func TestUsage(t *testing.T) {
	out := captureStdout(t, usage)
	if !strings.Contains(out, version) {
		t.Errorf("usage() does not contain version %q:\n%s", version, out)
	}
	for _, want := range []string{"mago init", "mago task", "mago worker", "Account (platform):", "Company (local/worker):"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage() missing %q:\n%s", want, out)
		}
	}
}
