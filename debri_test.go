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

func TestRunDebri(t *testing.T) {
	tmp := t.TempDir()
	debriBin := filepath.Join(tmp, "debri")
	body := `#!/bin/sh
printf '%s\n' '{"event":"init","status":"ok"}' '{"event":"done","content":"hello from debri","elapsed_ms":1}'
`
	if err := os.WriteFile(debriBin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake debri: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	workspace := t.TempDir()
	got, err := runDebri(workspace, &Agent{}, "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("runDebri error: %v", err)
	}
	if got != "hello from debri" {
		t.Errorf("runDebri = %q, want %q", got, "hello from debri")
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
