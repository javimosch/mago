package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
	msg, ftype = parseFeedbackArgs([]string{"--kind", "bug", "relay", "fails"})
	if msg != "relay fails" || ftype != "bug" {
		t.Errorf("--kind parse: msg=%q type=%q", msg, ftype)
	}
	msg, ftype = parseFeedbackArgs([]string{"-k", "feature", "csv", "export"})
	if msg != "csv export" || ftype != "feature" {
		t.Errorf("-k parse: msg=%q type=%q", msg, ftype)
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
	t.Setenv("USER", "   ")
	if got := reporter(); got != "agent" {
		t.Errorf("reporter with whitespace-only USER = %q, want agent", got)
	}
}

func TestGenFeedbackID(t *testing.T) {
	id1 := genFeedbackID()
	id2 := genFeedbackID()
	if len(id1) == 0 || id1 == id2 {
		t.Errorf("genFeedbackID returned empty or duplicate: %q, %q", id1, id2)
	}
}

func TestGenFeedbackID_EntropyFailure(t *testing.T) {
	orig := rand.Reader
	rand.Reader = &failReader{err: errors.New("entropy failure")}
	defer func() { rand.Reader = orig }()

	id := genFeedbackID()
	if len(id) != 32 {
		t.Errorf("fallback id length = %d, want 32", len(id))
	}
}

type failReader struct {
	err error
}

func (r *failReader) Read(p []byte) (int, error) {
	return 0, r.err
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

func TestCmdFeedback_EmptyMessage(t *testing.T) {
	if err := cmdFeedback([]string{}); err == nil {
		t.Fatal("expected error for empty feedback message")
	}
}

// TestCmdFeedback_RelaySuccess verifies a successful relay write is reported in
// the command's JSON output even when the platform endpoint is not authenticated.
func TestCmdFeedback_RelaySuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "tester")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/feedback" {
			t.Errorf("unexpected relay path: %q", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("FEEDBACK_RELAY", srv.URL)

	out := captureStdout(t, func() {
		if err := cmdFeedback([]string{"relay", "is", "working"}); err != nil {
			t.Fatalf("cmdFeedback: %v", err)
		}
	})
	if !strings.Contains(out, `"relayed":1`) {
		t.Errorf("expected relayed=1, got: %q", out)
	}
	if !strings.Contains(out, `"stored":0`) {
		t.Errorf("expected stored=0 when not logged in, got: %q", out)
	}
}

// TestCmdFeedback_PlatformAndRelaySuccess verifies that the platform endpoint
// and the central relay can both succeed in the same call, setting stored=1
// and relayed=1 in the JSON response.
func TestCmdFeedback_PlatformAndRelaySuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "tester")
	os.MkdirAll(home+"/.mago", 0o755)

	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/feedback" && r.Method == "POST" {
			if r.Header.Get("Authorization") != "Bearer platform-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"ok":true,"issue_url":"https://github.com/acme/mago/issues/1"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer platform.Close()

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/feedback" && r.Method == "POST" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer relay.Close()

	os.WriteFile(home+"/.mago/config.json", []byte(`{"token":"platform-token","platform_url":"`+platform.URL+`"}`), 0o600)
	t.Setenv("FEEDBACK_RELAY", relay.URL)

	out := captureStdout(t, func() {
		if err := cmdFeedback([]string{"stored", "and", "relayed"}); err != nil {
			t.Fatalf("cmdFeedback: %v", err)
		}
	})

	if !strings.Contains(out, `"stored":1`) {
		t.Errorf("expected stored=1, got: %q", out)
	}
	if !strings.Contains(out, `"relayed":1`) {
		t.Errorf("expected relayed=1, got: %q", out)
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

	if postFeedback("http://[::1]:0/invalid", body) {
		t.Error("postFeedback on invalid URL should return false")
	}
}

// TestPostFeedback_UnparseableURL covers the http.NewRequest error branch: a URL
// that fails to parse must return false rather than panic.
func TestPostFeedback_UnparseableURL(t *testing.T) {
	if postFeedback("http://exa mple/v1/feedback", map[string]any{"message": "x"}) {
		t.Error("postFeedback with an unparseable URL should return false")
	}
}
