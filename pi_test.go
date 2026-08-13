package main

import (
	"io"
	"os"
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
