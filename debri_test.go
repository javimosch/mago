package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmitProgressDebri(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	emitProgressDebri(`{"event":"chunk","content":"hello "}`)
	emitProgressDebri(`{"event":"chunk","content":"world"}`)
	emitProgressDebri(`{"event":"init","status":"ok"}`) // non-chunk: no output
	emitProgressDebri("not valid json")                 // unparseable: no output

	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	got := string(out)
	want := "hello world"
	if got != want {
		t.Errorf("emitProgressDebri wrote %q, want %q", got, want)
	}
}

func TestDebriModel(t *testing.T) {
	if got := debriModel(&Agent{Model: "SWE-1.6"}); got != "SWE-1.6" {
		t.Errorf("debriModel with a set model: got %q, want %q", got, "SWE-1.6")
	}
	if got := debriModel(&Agent{Model: "  "}); got != "" {
		t.Errorf("debriModel with a blank model should return empty (devin default), got %q", got)
	}
	if got := debriModel(&Agent{}); got != "" {
		t.Errorf("debriModel with no model should return empty (devin default), got %q", got)
	}
}

func TestCombineDebriPrompt(t *testing.T) {
	got := combineDebriPrompt("PERSONA RULES", "BRIEFING TASK")
	if !strings.Contains(got, "PERSONA RULES") || !strings.Contains(got, "BRIEFING TASK") {
		t.Fatalf("combined prompt missing a part: %q", got)
	}
	if strings.Index(got, "PERSONA RULES") > strings.Index(got, "BRIEFING TASK") {
		t.Errorf("system prompt must come before the briefing: %q", got)
	}
	// The reflection-format reminder must come LAST — that's the whole point (reinforce the
	// requirement right before generation, where a model's attention is freshest).
	if strings.Index(got, "BRIEFING TASK") > strings.Index(got, "fenced ```json") {
		t.Errorf("reflection reminder must come after the briefing: %q", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "for a small or already-complete task.") {
		t.Errorf("reflection reminder must be the final thing in the prompt: %q", got)
	}
}

func TestExtractDebriFinalContent_DoneLine(t *testing.T) {
	lines := []string{
		`{"event":"init","status":"ok"}`,
		`{"event":"chunk","content":"hello "}`,
		`{"event":"done","content":"final-content","elapsed_ms":123}`,
	}
	got, err := extractDebriFinalContent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "final-content" {
		t.Errorf("got %q, want %q", got, "final-content")
	}
}

func TestExtractDebriFinalContent_FallbackChunks(t *testing.T) {
	// A done event with no content field falls back to the concatenated chunks.
	lines := []string{
		`{"event":"chunk","content":"hel"}`,
		`{"event":"chunk","content":"lo"}`,
		`{"event":"done","content":"","elapsed_ms":1}`,
	}
	got, err := extractDebriFinalContent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestExtractDebriFinalContent_ChunksNoDone(t *testing.T) {
	// If debri exits without ever emitting a {"event":"done"} line, but it did stream
	// chunks, we should still return the concatenated output rather than discarding it.
	lines := []string{
		`{"event":"chunk","content":"hel"}`,
		`{"event":"chunk","content":"lo"}`,
	}
	got, err := extractDebriFinalContent(lines)
	if err != nil {
		t.Fatalf("unexpected error for chunk-only output: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestExtractDebriFinalContent_ErrorEvent(t *testing.T) {
	// Critical behavior enabled by debri v1.1.0: a crashed/killed devin session is a real
	// {"event":"error"}, not a false {"event":"done"} with empty content — the caller must
	// surface it as an error, not silently treat it as an empty-but-successful tick.
	lines := []string{
		`{"event":"init","status":"ok"}`,
		`{"event":"error","error":"devin tmux session disappeared unexpectedly after 12 polls (crashed, killed externally, or host issue)","elapsed_ms":3000}`,
	}
	_, err := extractDebriFinalContent(lines)
	if err == nil {
		t.Fatal("expected an error for an {\"event\":\"error\"} line, got nil")
	}
	if !strings.Contains(err.Error(), "disappeared unexpectedly") {
		t.Errorf("error should carry debri's own message, got: %v", err)
	}
}

func TestExtractDebriFinalContent_LegitimatelyEmptyDone(t *testing.T) {
	// A done event with genuinely no output (and no chunks) is NOT an error — some ticks
	// produce no assistant text (e.g. pure tool use with a silent final turn).
	lines := []string{`{"event":"done","content":"","elapsed_ms":50}`}
	got, err := extractDebriFinalContent(lines)
	if err != nil {
		t.Fatalf("unexpected error for legitimately empty done: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestExtractDebriFinalContent_NoOutput(t *testing.T) {
	_, err := extractDebriFinalContent(nil)
	if err == nil {
		t.Fatal("expected error for no lines at all, got nil")
	}
}

// TestExtractDebriFinalContent_SkipsNoise verifies blank lines and non-JSON noise in the
// NDJSON stream are skipped rather than aborting the parse or corrupting the result.
func TestExtractDebriFinalContent_SkipsNoise(t *testing.T) {
	lines := []string{
		"",
		"   ",
		"this is not json",
		`{"event":"chunk","content":"x"}`,
		`{"event":"done","content":"final","elapsed_ms":5}`,
	}
	got, err := extractDebriFinalContent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "final" {
		t.Errorf("got %q, want %q", got, "final")
	}
}

func TestDebriJSONResult_Success(t *testing.T) {
	got, err := debriJSONResult([]byte(`{"content":"the answer","elapsed_ms":42}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "the answer" {
		t.Errorf("got %q, want %q", got, "the answer")
	}
}

func TestDebriJSONResult_Error(t *testing.T) {
	_, err := debriJSONResult([]byte(`{"error":"devin did not start within 300s","elapsed_ms":300000}`))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "did not start") {
		t.Errorf("error should carry debri's own message, got: %v", err)
	}
}

func TestDebriJSONResult_Garbled(t *testing.T) {
	_, err := debriJSONResult([]byte("not json at all"))
	if err == nil {
		t.Fatal("expected an error for unparseable output, got nil")
	}
}

func TestWriteDebriPromptFile(t *testing.T) {
	path, cleanup, err := writeDebriPromptFile("hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written prompt file: %v", err)
	}
	if string(b) != "hello world" {
		t.Errorf("got %q, want %q", string(b), "hello world")
	}
	cleanup()
	if _, err := os.ReadFile(path); err == nil {
		t.Error("cleanup should have removed the temp file")
	}
}

func TestWriteDebriPromptFile_CreateTempError(t *testing.T) {
	// If os.CreateTemp cannot create a file (e.g. TMPDIR points at a missing directory),
	// writeDebriPromptFile must surface the error with a nil cleanup and no partial state.
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))
	path, cleanup, err := writeDebriPromptFile("hello")
	if err == nil {
		t.Fatal("expected an error when temp dir is missing, got nil")
	}
	if path != "" {
		t.Errorf("expected empty path on error, got %q", path)
	}
	if cleanup != nil {
		t.Error("expected nil cleanup on error")
	}
}

// TestRunDebri_ErrorEvent verifies that runDebri surfaces a debri-level {"event":"error"}
// as a real error rather than treating it as an empty-but-successful tick.
func TestRunDebri_ErrorEvent(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "debri")
	body := "#!/bin/sh\necho '{\"event\":\"error\",\"error\":\"tmux session disappeared\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	a := &Agent{Model: "SWE-1.6"}
	_, err := runDebri(t.TempDir(), a, "system prompt", "user prompt")
	if err == nil {
		t.Fatal("runDebri: expected an error for an error event, got nil")
	}
	if !strings.Contains(err.Error(), "tmux session disappeared") {
		t.Errorf("runDebri should surface debri's own error message, got: %v", err)
	}
}

// TestRunDebri_Success exercises the full tick path with a fake debri binary on PATH.
// A v1.1.0+ debri emits NDJSON and reports a done event; runDebri should return the
// done content without error.
func TestRunDebri_Success(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "debri")
	body := "#!/bin/sh\necho '{\"event\":\"done\",\"content\":\"reflection result\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	a := &Agent{Model: "SWE-1.6"}
	got, err := runDebri(t.TempDir(), a, "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("runDebri: unexpected error: %v", err)
	}
	if got != "reflection result" {
		t.Errorf("got %q, want %q", got, "reflection result")
	}
}

// TestRunDebri_NotOnPath verifies that runDebri returns a clear integration error
// when the debri binary is not on PATH, rather than a raw exec error.
func TestRunDebri_NotOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	a := &Agent{Model: "SWE-1.6"}
	_, err := runDebri(t.TempDir(), a, "system prompt", "user prompt")
	if err == nil {
		t.Fatal("runDebri: expected an error when debri is not on PATH")
	}
	if !strings.Contains(err.Error(), "starting debri") || !strings.Contains(err.Error(), "PATH") {
		t.Errorf("runDebri should surface a PATH hint, got: %v", err)
	}
}

// TestRunDebri_EmptyDoneNonZeroExit verifies the case where debri emits a legitimate
// {"event":"done"} with no content (not a debri-reported error) but the process still
// exits non-zero: the wait error must be surfaced rather than swallowed as empty success.
func TestRunDebri_EmptyDoneNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "debri")
	body := "#!/bin/sh\necho '{\"event\":\"done\",\"content\":\"\"}'\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	a := &Agent{Model: "SWE-1.6"}
	_, err := runDebri(t.TempDir(), a, "system prompt", "user prompt")
	if err == nil {
		t.Fatal("runDebri: expected an error for empty done + non-zero exit, got nil")
	}
	if !strings.Contains(err.Error(), "debri failed") {
		t.Errorf("runDebri should surface the wait error, got: %v", err)
	}
}

// TestRunDebri_PromptFileError verifies runDebri wraps a prompt-file write failure
// (e.g. TMPDIR pointing at a missing directory) instead of exec'ing debri.
func TestRunDebri_PromptFileError(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))

	a := &Agent{Model: "SWE-1.6"}
	_, err := runDebri(t.TempDir(), a, "system prompt", "user prompt")
	if err == nil {
		t.Fatal("runDebri: expected an error when the prompt file cannot be written")
	}
	if !strings.Contains(err.Error(), "writing prompt file") {
		t.Errorf("runDebri should wrap the prompt-file error, got: %v", err)
	}
}
