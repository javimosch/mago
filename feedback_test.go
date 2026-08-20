package main

import (
	"net/http"
	"net/http/httptest"
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
