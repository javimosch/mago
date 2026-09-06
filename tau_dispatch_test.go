package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runTau dispatches on the agent's provider: "claude" goes to the Claude Code
// harness instead of shelling out to tau.
func TestRunTau_ClaudeDispatch(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	body := "#!/bin/sh\ncat >/dev/null 2>/dev/null\necho '{\"result\": \"done\", \"is_error\": false, \"subtype\": \"\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	got, err := runTau(t.TempDir(), &Agent{Provider: "claude", Model: "sonnet"}, "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("runTau: unexpected error: %v", err)
	}
	if got != "done" {
		t.Errorf("runTau = %q, want %q", got, "done")
	}
}

// runTau dispatches provider "debri" to the devin harness instead of tau.
func TestRunTau_DebriDispatch(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "debri")
	body := "#!/bin/sh\necho '{\"event\":\"done\",\"content\":\"reflection result\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got, err := runTau(t.TempDir(), &Agent{Provider: "debri", Model: "SWE-1.6"}, "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("runTau: unexpected error: %v", err)
	}
	if got != "reflection result" {
		t.Errorf("runTau = %q, want %q", got, "reflection result")
	}
}

// When tau exits non-zero and produced no content, runTau surfaces the wait
// error ("tau failed: ...") rather than the empty-output parse error.
func TestRunTau_FailedExit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tau")
	body := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	_, err := runTau(t.TempDir(), &Agent{Provider: "opencode", Model: "qwen2.5"}, "system prompt", "user prompt")
	if err == nil {
		t.Fatal("runTau: expected an error when tau exits non-zero")
	}
	if !strings.Contains(err.Error(), "tau failed") {
		t.Errorf("runTau should surface 'tau failed', got: %v", err)
	}
}

// When tau exits cleanly but emitted no parseable content, runTau surfaces the
// extractFinalContent error ("no output from tau").
func TestRunTau_NoOutput(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tau")
	body := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	_, err := runTau(t.TempDir(), &Agent{Provider: "opencode", Model: "qwen2.5"}, "system prompt", "user prompt")
	if err == nil {
		t.Fatal("runTau: expected an error for empty output")
	}
	if !strings.Contains(err.Error(), "no output from tau") {
		t.Errorf("runTau should surface 'no output from tau', got: %v", err)
	}
}

// tauComplete dispatches provider "debri" to debriComplete instead of tau.
func TestTauComplete_DebriDispatch(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "debri")
	body := "#!/bin/sh\necho '{\"content\":\"the answer\",\"elapsed_ms\":1}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got, err := tauComplete(&Agent{Provider: "debri", Model: "SWE-1.6"}, "prompt")
	if err != nil {
		t.Fatalf("tauComplete: unexpected error: %v", err)
	}
	if got != "the answer" {
		t.Errorf("tauComplete = %q, want %q", got, "the answer")
	}
}

// When tau succeeds but emits no parseable JSON line, tauComplete retries and
// finally returns "no content from tau".
func TestTauComplete_NoContent(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tau")
	body := "#!/bin/sh\necho 'not json'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	_, err := tauComplete(&Agent{Provider: "opencode", Model: "qwen2.5"}, "prompt")
	if err == nil {
		t.Fatal("tauComplete: expected an error for unparseable output")
	}
	if !strings.Contains(err.Error(), "no content from tau") {
		t.Errorf("tauComplete should surface 'no content from tau', got: %v", err)
	}
}

// extractFinalContent skips blank and non-JSON lines instead of choking on them.
func TestExtractFinalContent_SkipsBlankAndMalformed(t *testing.T) {
	lines := []string{"", "   ", "not json at all", `{"done":true,"content":"final"}`}
	got, err := extractFinalContent(lines)
	if err != nil {
		t.Fatalf("extractFinalContent: unexpected error: %v", err)
	}
	if got != "final" {
		t.Errorf("extractFinalContent = %q, want %q", got, "final")
	}
}
