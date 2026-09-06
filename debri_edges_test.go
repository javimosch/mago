package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDebri drops an executable `debri` shell script into a temp dir and makes it
// the only thing on PATH for the test, matching the existing debri tests.
func fakeDebri(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "debri"), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write fake debri: %v", err)
	}
	t.Setenv("PATH", dir)
}

// TestRunDebri_PromptFileError verifies runDebri wraps a writeDebriPromptFile
// failure (e.g. a missing TMPDIR) with its "debri: writing prompt file" context.
func TestRunDebri_PromptFileError(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))

	_, err := runDebri(t.TempDir(), &Agent{Model: "SWE-1.6"}, "sys", "user")
	if err == nil {
		t.Fatal("runDebri: expected an error when the prompt file can't be written")
	}
	if !strings.Contains(err.Error(), "writing prompt file") {
		t.Errorf("runDebri should wrap the prompt-file error, got: %v", err)
	}
}

// TestRunDebri_ExitErrorEmptyOutput verifies that a debri process which exits
// non-zero after emitting a done event with no content surfaces the wait error —
// the session produced nothing AND failed.
func TestRunDebri_ExitErrorEmptyOutput(t *testing.T) {
	fakeDebri(t, "echo '{\"event\":\"done\",\"content\":\"\"}'\nexit 1\n")

	_, err := runDebri(t.TempDir(), &Agent{Model: "SWE-1.6"}, "sys", "user")
	if err == nil {
		t.Fatal("runDebri: expected an error for empty output + non-zero exit")
	}
	if !strings.Contains(err.Error(), "debri failed") {
		t.Errorf("runDebri should surface the wait error, got: %v", err)
	}
}

// TestDebriComplete_PromptFileError verifies debriComplete wraps a
// writeDebriPromptFile failure rather than proceeding to exec debri.
func TestDebriComplete_PromptFileError(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))

	_, err := debriComplete(&Agent{Model: "SWE-1.6"}, "prompt")
	if err == nil {
		t.Fatal("debriComplete: expected an error when the prompt file can't be written")
	}
	if !strings.Contains(err.Error(), "writing prompt file") {
		t.Errorf("debriComplete should wrap the prompt-file error, got: %v", err)
	}
}

// TestDebriComplete_UnparseableOutput verifies that debri output with no JSON
// result at all is recorded as the retry loop's lastErr (the cerr branch) and
// returned after all attempts fail.
func TestDebriComplete_UnparseableOutput(t *testing.T) {
	fakeDebri(t, "echo 'not json at all'\n")

	_, err := debriComplete(&Agent{Model: "SWE-1.6"}, "prompt")
	if err == nil {
		t.Fatal("debriComplete: expected an error for unparseable output")
	}
	if !strings.Contains(err.Error(), "no parseable JSON") {
		t.Errorf("debriComplete should surface the parse error, got: %v", err)
	}
}

// TestDebriComplete_EmptyContentNoError covers the final lastErr fallback: a
// clean exit whose JSON result simply carries no content is an "empty content"
// failure, retried like any other.
func TestDebriComplete_EmptyContentNoError(t *testing.T) {
	fakeDebri(t, "echo '{\"content\":\"\",\"elapsed_ms\":1}'\n")

	_, err := debriComplete(&Agent{Model: "SWE-1.6"}, "prompt")
	if err == nil {
		t.Fatal("debriComplete: expected an error for empty content")
	}
	if !strings.Contains(err.Error(), "empty content") {
		t.Errorf("debriComplete should report empty content, got: %v", err)
	}
}

// TestExtractDebriFinalContent_SkipsBlankAndGarbage verifies that blank lines and
// non-JSON noise in the NDJSON stream are skipped rather than aborting the parse.
func TestExtractDebriFinalContent_SkipsBlankAndGarbage(t *testing.T) {
	got, err := extractDebriFinalContent([]string{
		"",
		"   ",
		"not json",
		"{broken",
		`{"event":"done","content":"real result"}`,
	})
	if err != nil {
		t.Fatalf("extractDebriFinalContent: unexpected error: %v", err)
	}
	if got != "real result" {
		t.Errorf("got %q, want %q", got, "real result")
	}
}
