package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractPiFinalContent(t *testing.T) {
	// turn_end assistant message
	turn := `{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"turn reply"}]}}`
	if got, err := extractPiFinalContent([]string{turn}); err != nil || got != "turn reply" {
		t.Errorf("turn_end: got %q, err %v", got, err)
	}

	// message_end assistant message
	msg := `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"message reply"}]}}`
	if got, err := extractPiFinalContent([]string{msg}); err != nil || got != "message reply" {
		t.Errorf("message_end: got %q, err %v", got, err)
	}

	// agent_end takes the last assistant message in the messages array
	agent := `{"type":"agent_end","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]},{"role":"assistant","content":[{"type":"text","text":"hello"}]}]}`
	if got, err := extractPiFinalContent([]string{agent}); err != nil || got != "hello" {
		t.Errorf("agent_end: got %q, err %v", got, err)
	}

	// The most terminal event wins when multiple are present (reversed search).
	mixed := []string{
		turn,
		agent,
	}
	if got, err := extractPiFinalContent(mixed); err != nil || got != "hello" {
		t.Errorf("reversed terminal search: got %q, err %v", got, err)
	}

	// Ignore non-assistant roles.
	noAssistant := `{"type":"agent_end","messages":[{"role":"user","content":[{"type":"text","text":"ignored"}]}]}`
	if got, err := extractPiFinalContent([]string{noAssistant}); err == nil {
		t.Errorf("non-assistant content should fail, got %q", got)
	}

	// Empty lines are skipped.
	if got, err := extractPiFinalContent([]string{"", " "}); err == nil {
		t.Errorf("empty output should fail, got %q", got)
	}
}

func TestEmitProgressPi(t *testing.T) {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = old
		w.Close()
	})

	// Non-matching events produce no output.
	emitProgressPi(`{"type":"turn_end"}`)
	emitProgressPi(`not json`)

	// text_delta events are written to stderr.
	emitProgressPi(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello"}}`)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "hello" {
		t.Errorf("emitProgressPi wrote %q, want %q", got, "hello")
	}
}

// piComplete is the no-tools lightweight completion path.
func TestPiComplete(t *testing.T) {
	tmp := t.TempDir()
	piBin := filepath.Join(tmp, "pi")
	json := `{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"complete result"}]}]}`
	script := "#!/bin/sh\nprintf '%s\\n' '" + json + `'` + "\n"
	if err := os.WriteFile(piBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	a := &Agent{Model: "openrouter/test"}
	got, err := piComplete(a, "prompt")
	if err != nil {
		t.Fatalf("piComplete error: %v", err)
	}
	if got != "complete result" {
		t.Errorf("piComplete returned %q, want %q", got, "complete result")
	}
}

// runPi streams the pi CLI and extracts the final assistant content.
func TestRunPi(t *testing.T) {
	tmp := t.TempDir()
	piBin := filepath.Join(tmp, "pi")
	json := `{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"hello from pi"}]}]}`
	script := "#!/bin/sh\nprintf '%s\\n' '" + json + `'` + "\n"
	if err := os.WriteFile(piBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	a := &Agent{Model: "openrouter/test"}
	got, err := runPi(tmp, a, "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("runPi error: %v", err)
	}
	if got != "hello from pi" {
		t.Errorf("runPi returned %q, want %q", got, "hello from pi")
	}
}

// TestRunPi_NotOnPath verifies that runPi returns a clear integration error
// when the pi binary is not on PATH.
func TestRunPi_NotOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	a := &Agent{Model: "openrouter/test"}
	_, err := runPi(t.TempDir(), a, "system prompt", "user prompt")
	if err == nil {
		t.Fatal("runPi: expected an error when pi is not on PATH")
	}
	if !strings.Contains(err.Error(), "starting pi") || !strings.Contains(err.Error(), "PATH") {
		t.Errorf("runPi should surface a PATH hint, got: %v", err)
	}
}

// TestRunPi_NoOutput verifies that runPi returns an error when the pi binary
// exits successfully but emits no parseable assistant content.
func TestRunPi_NoOutput(t *testing.T) {
	dir := t.TempDir()
	piBin := filepath.Join(dir, "pi")
	body := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(piBin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	a := &Agent{Model: "openrouter/test"}
	_, err := runPi(t.TempDir(), a, "system prompt", "user prompt")
	if err == nil {
		t.Fatal("runPi: expected an error for empty output")
	}
	if !strings.Contains(err.Error(), "no output from pi") {
		t.Errorf("runPi should surface 'no output from pi', got: %v", err)
	}
}
