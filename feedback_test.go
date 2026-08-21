package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseFeedbackArgs(t *testing.T) {
	msg, ftype := parseFeedbackArgs([]string{"worker", "doctor", "is", "confusing"})
	if msg != "worker doctor is confusing" || ftype != "feedback" {
		t.Errorf("default type: msg=%q type=%q", msg, ftype)
	}
	msg, ftype = parseFeedbackArgs([]string{"--type", "bug", "relay", "drops", "events"})
	if msg != "relay drops events" || ftype != "bug" {
		t.Errorf("--type parse: msg=%q type=%q", msg, ftype)
	}
	msg, ftype = parseFeedbackArgs([]string{"add", "a", "-t", "feature", "csv", "export"})
	if msg != "add a csv export" || ftype != "feature" {
		t.Errorf("-t mid-args: msg=%q type=%q", msg, ftype)
	}
	msg, ftype = parseFeedbackArgs([]string{"--context", "running loop", "worker", "doctor", "is", "confusing"})
	if msg != "worker doctor is confusing" || ftype != "feedback" {
		t.Errorf("--context should not appear in message: msg=%q type=%q", msg, ftype)
	}
	if m, _ := parseFeedbackArgs(nil); m != "" {
		t.Errorf("empty args -> empty msg, got %q", m)
	}
}

func TestParseContext(t *testing.T) {
	if got := parseContext([]string{"--context", "running loop", "some", "message"}); got != "running loop" {
		t.Errorf("parseContext = %q", got)
	}
	if got := parseContext([]string{"some", "message"}); got != "" {
		t.Errorf("parseContext without flag = %q", got)
	}
}

func TestReporter(t *testing.T) {
	t.Setenv("USER", "tester")
	if got := reporter(); got != "tester" {
		t.Errorf("reporter with USER = %q", got)
	}
	t.Setenv("USER", "")
	if got := reporter(); got != "agent" {
		t.Errorf("reporter without USER = %q", got)
	}
}

func TestGenFeedbackID(t *testing.T) {
	id1 := genFeedbackID()
	id2 := genFeedbackID()
	if len(id1) == 0 || id1 == id2 {
		t.Errorf("genFeedbackID returned empty or duplicate: %q, %q", id1, id2)
	}
}

// TestCmdFeedback_WhenOffline verifies the full `mago feedback` command is
// best-effort: it prints a JSON ok response with stored/relayed zeroes when not
// logged in and FEEDBACK_RELAY=off.
func TestCmdFeedback_WhenOffline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("FEEDBACK_RELAY", "off")

	out := captureStdout(t, func() {
		if err := cmdFeedback([]string{"this", "is", "a", "test"}); err != nil {
			t.Fatalf("cmdFeedback: %v", err)
		}
	})
	if !strings.Contains(out, `"ok":true`) {
		t.Errorf("expected ok=true, got: %q", out)
	}
	if !strings.Contains(out, `"stored":0`) {
		t.Errorf("expected stored=0 when not logged in, got: %q", out)
	}
	if !strings.Contains(out, `"relayed":0`) {
		t.Errorf("expected relayed=0 with relay off, got: %q", out)
	}
	if !strings.Contains(out, `"id":"`) {
		t.Errorf("expected id in output, got: %q", out)
	}
}

func TestPostFeedback(t *testing.T) {
	body := map[string]any{"message": "hello"}

	called := false
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer okSrv.Close()

	if !postFeedback(okSrv.URL, body) {
		t.Error("postFeedback on 200 should return true")
	}
	if !called {
		t.Error("server was never called")
	}

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failSrv.Close()

	if postFeedback(failSrv.URL, body) {
		t.Error("postFeedback on 500 should return false")
	}
}
