package main

import (
	"os"
	"path/filepath"
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

// TestMain_Dispatch exercises the top-level command router for commands that
// return without calling os.Exit: version, help, and a simple init. This keeps
// the dispatch switch covered as new commands are added.
func TestMain_Dispatch(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// version just prints the version and returns.
	os.Args = []string{"mago", "version"}
	out := captureStdout(t, main)
	if !strings.Contains(out, version) {
		t.Errorf("version output = %q, want %q", out, version)
	}

	// help prints usage and returns.
	os.Args = []string{"mago", "help"}
	out = captureStdout(t, main)
	if !strings.Contains(out, "mago init") {
		t.Errorf("help output missing 'mago init':\n%s", out)
	}

	// init creates a company and returns without an error.
	dir := t.TempDir()
	os.Args = []string{"mago", "init", dir}
	out = captureStdout(t, main)
	if !strings.Contains(out, "initialized mago company") {
		t.Errorf("init output = %q, want 'initialized mago company'", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mago")); err != nil {
		t.Errorf("init did not create .mago/: %v", err)
	}

	// feedback returns without failing the caller even when offline, and emits
	// the JSON response expected by agents driving the CLI.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USER", "tester")
	t.Setenv("FEEDBACK_RELAY", "off")
	os.Args = []string{"mago", "feedback", "dispatch", "test"}
	out = captureStdout(t, main)
	if !strings.Contains(out, `"ok":true`) {
		t.Errorf("feedback output missing ok=true: %q", out)
	}
	if !strings.Contains(out, `"relayed":0`) {
		t.Errorf("feedback output should show relayed=0 with FEEDBACK_RELAY=off: %q", out)
	}
}
