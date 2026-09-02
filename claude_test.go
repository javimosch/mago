package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOverloadish(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"overloaded, try again later", true},
		{"rate limit exceeded", true},
		{"timeout waiting for model", true},
		{"timed out after 30s", true},
		{"503 service unavailable", true},
		{"529 server is overloaded", true},
		{"connection reset by peer", true},
		{"unknown genuine error", false},
		{"", false},
	}
	for _, c := range cases {
		if got := overloadish(c.msg); got != c.want {
			t.Errorf("overloadish(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

func TestClaudeModel(t *testing.T) {
	// Explicit model is respected.
	if got := claudeModel(&Agent{Model: "opus"}); got != "opus" {
		t.Errorf("claudeModel(Model=opus) = %q, want opus", got)
	}
	// Whitespace-only model falls back to the default.
	if got := claudeModel(&Agent{Model: "  "}); got != "sonnet" {
		t.Errorf("claudeModel(Model='  ') = %q, want sonnet", got)
	}
	// Empty model falls back to sonnet.
	if got := claudeModel(&Agent{}); got != "sonnet" {
		t.Errorf("claudeModel({}) = %q, want sonnet", got)
	}
}

func TestClaudeResultClassification(t *testing.T) {
	// Garbled / non-JSON -> transient.
	if _, err := claudeResult([]byte("boom not json")); !transientClaude(err) {
		t.Errorf("garbled output should be transient, got %v", err)
	}
	// Empty result -> transient.
	if _, err := claudeResult([]byte(`{"result":"","is_error":false}`)); !transientClaude(err) {
		t.Errorf("empty result should be transient, got %v", err)
	}
	// Overload message -> transient.
	if _, err := claudeResult([]byte(`{"result":"API Error: Overloaded","is_error":true}`)); !transientClaude(err) {
		t.Errorf("overload should be transient, got %v", err)
	}
	// Auth failure -> NOT transient (don't waste retries).
	if _, err := claudeResult([]byte(`{"result":"Not logged in","is_error":true}`)); err == nil || transientClaude(err) {
		t.Errorf("auth error must be non-transient, got %v", err)
	}
	// Genuine model error -> NOT transient.
	if _, err := claudeResult([]byte(`{"result":"the code has a bug","is_error":true,"subtype":"x"}`)); err == nil || transientClaude(err) {
		t.Errorf("real error must be non-transient, got %v", err)
	}
	// Success.
	if r, err := claudeResult([]byte(`{"result":"ok","is_error":false}`)); err != nil || r != "ok" {
		t.Errorf("success parse: %q %v", r, err)
	}
}

func TestWithClaudeRetry(t *testing.T) {
	// Retries transient until success.
	n := 0
	r, err := withClaudeRetry(4, time.Millisecond, func() (string, error) {
		n++
		if n < 3 {
			return "", errClaudeTransient
		}
		return "done", nil
	})
	if err != nil || r != "done" || n != 3 {
		t.Errorf("should retry transient to success: r=%q n=%d err=%v", r, n, err)
	}

	// Non-transient bails immediately (one call).
	n = 0
	_, err = withClaudeRetry(4, time.Millisecond, func() (string, error) {
		n++
		return "", errors.New("not authenticated")
	})
	if err == nil || n != 1 {
		t.Errorf("non-transient should not retry: n=%d err=%v", n, err)
	}
}

func TestClaudeComplete(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	body := "#!/bin/sh\ncat >/dev/null 2>/dev/null\necho '{\"result\": \"completed\", \"is_error\": false, \"subtype\": \"\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	got, err := claudeComplete(&Agent{Model: "sonnet"}, "prompt")
	if err != nil {
		t.Fatalf("claudeComplete: %v", err)
	}
	if got != "completed" {
		t.Errorf("claudeComplete = %q, want completed", got)
	}
}

// TestClaudeComplete_CommandErrorStillParsesResult verifies that claudeComplete
// still extracts a valid result from stdout when the claude CLI exits non-zero,
// hitting the runErr != nil log path rather than failing immediately.
func TestClaudeComplete_CommandErrorStillParsesResult(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	body := "#!/bin/sh\ncat >/dev/null 2>/dev/null\necho '{\"result\": \"completed\", \"is_error\": false, \"subtype\": \"\"}'\nexit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	got, err := claudeComplete(&Agent{Model: "sonnet"}, "prompt")
	if err != nil {
		t.Fatalf("claudeComplete: %v", err)
	}
	if got != "completed" {
		t.Errorf("claudeComplete = %q, want completed despite non-zero exit", got)
	}
}

func TestRunClaude(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	body := "#!/bin/sh\ncat >/dev/null 2>/dev/null\necho '{\"result\": \"done\", \"is_error\": false, \"subtype\": \"\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	workspace := t.TempDir()
	got, err := runClaude(workspace, &Agent{}, "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("runClaude: %v", err)
	}
	if got != "done" {
		t.Errorf("runClaude = %q, want done", got)
	}
}
