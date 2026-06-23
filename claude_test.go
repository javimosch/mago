package main

import (
	"errors"
	"testing"
	"time"
)

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
