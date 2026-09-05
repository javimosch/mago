package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWorkerID_EnvOverride(t *testing.T) {
	t.Setenv("MAGO_WORKER_ID", "edge-runner-01")
	if got := workerID(); got != "edge-runner-01" {
		t.Errorf("workerID() = %q, want edge-runner-01", got)
	}
}

func TestWorkerID_FallbackToHostname(t *testing.T) {
	t.Setenv("MAGO_WORKER_ID", "")
	got := workerID()
	if got == "" {
		t.Error("workerID() should not be empty when env is unset")
	}
	// If hostname works, it should match; if not, it falls back to "worker".
	if h, err := os.Hostname(); err == nil && h != "" && got != h {
		t.Errorf("workerID() = %q, want hostname %q", got, h)
	}
}

func TestWorkerID_TrimSpace(t *testing.T) {
	t.Setenv("MAGO_WORKER_ID", "  edge-runner-01  ")
	if got := workerID(); got != "edge-runner-01" {
		t.Errorf("workerID() = %q, want trimmed env value %q", got, "edge-runner-01")
	}
}

// TestWorkerID_WhitespaceOnlyEnvFallsBack verifies that a whitespace-only
// MAGO_WORKER_ID is treated as unset and the function falls back to the
// hostname (or the hard-coded "worker" fallback).
func TestWorkerID_WhitespaceOnlyEnvFallsBack(t *testing.T) {
	t.Setenv("MAGO_WORKER_ID", "   ")
	got := workerID()
	if got == "   " {
		t.Errorf("workerID() = %q, should not return untrimmed whitespace", got)
	}
	if got == "" {
		t.Errorf("workerID() should not be empty")
	}
}

func TestRunRelay_NoLicense(t *testing.T) {
	// With no license key, runRelay should log once and return without dialing out.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	w := &eventWorker{}
	cfg := &cliConfig{}
	done := make(chan struct{})
	go func() {
		runRelay(ctx, w, cfg, []string{"owner/repo"})
		close(done)
	}()
	select {
	case <-done:
		// expected: early return
	case <-ctx.Done():
		t.Fatal("runRelay without a license key blocked instead of returning")
	}
}

func TestStreamRelay_Refused(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w := &eventWorker{}
	cfg := &cliConfig{PlatformURL: ts.URL, LicenseKey: "test-key"}
	err := streamRelay(ctx, w, cfg, []string{"owner/repo"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("streamRelay(401) = %v, want HTTP 401 error", err)
	}
}

// TestStreamRelay_HappyPath verifies that a 200 streaming response dispatches ping,
// control, and webhook events correctly: the control frame is applied to the company
// mode, and a webhook event is signalled to the worker.
func TestStreamRelay_HappyPath(t *testing.T) {
	c := newTestCompany(t)
	t.Setenv("MAGO_WORKER_ID", "relay-test")

	self := selfVersion()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/worker" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		f := w.(http.Flusher)
		fmt.Fprintf(w, "{\"event\":\"ping\",\"version\":\"%s\"}\n", self)
		f.Flush()
		fmt.Fprint(w, "{\"event\":\"control\",\"body\":{\"tokens\":[\"proactive=1200\"]}}\n")
		f.Flush()
		fmt.Fprint(w, "{\"event\":\"issues\",\"body\":{\"action\":\"opened\",\"issue\":{\"number\":42,\"labels\":[]}}}\n")
		f.Flush()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	w := &eventWorker{comp: c, wake: make(chan wakeEvent, 2)}
	cfg := &cliConfig{PlatformURL: srv.URL, LicenseKey: "test-key"}
	err := streamRelay(ctx, w, cfg, []string{"owner/repo"})
	if err == nil || !strings.Contains(err.Error(), "stream closed") {
		t.Errorf("streamRelay should end with stream closed, got: %v", err)
	}

	m := c.loadMode()
	if m.Proactive != 1200 {
		t.Errorf("control event did not update mode: proactive=%d, want 1200", m.Proactive)
	}

	select {
	case ev := <-w.wake:
		if !strings.Contains(ev.reason, "issue #42") {
			t.Errorf("wake reason = %q, want issue #42 mention", ev.reason)
		}
	default:
		t.Error("expected a wake event for the issues webhook")
	}
}

// TestStreamRelay_BadURL verifies that a platform URL that cannot be parsed into a
// request is surfaced as an error rather than panicking or hanging.
func TestStreamRelay_BadURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w := &eventWorker{}
	cfg := &cliConfig{PlatformURL: "http://\x01invalid", LicenseKey: "test-key"}
	if err := streamRelay(ctx, w, cfg, []string{"owner/repo"}); err == nil {
		t.Error("streamRelay with an unparseable platform URL should error")
	}
}

// TestStreamRelay_DialError verifies that an unreachable platform is reported as an
// error (a real failure), not confused with a clean stream close.
func TestStreamRelay_DialError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w := &eventWorker{}
	cfg := &cliConfig{PlatformURL: "http://127.0.0.1:1", LicenseKey: "test-key"}
	if err := streamRelay(ctx, w, cfg, []string{"owner/repo"}); err == nil {
		t.Error("streamRelay to an unreachable platform should error")
	}
}

// TestStreamRelay_SkipsNoise verifies that blank lines and non-JSON noise on the
// stream are skipped: the connection keeps reading and ends with "stream closed"
// rather than treating junk as an event or aborting.
func TestStreamRelay_SkipsNoise(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "\n   \nthis is not json\n")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w := &eventWorker{}
	cfg := &cliConfig{PlatformURL: srv.URL, LicenseKey: "test-key"}
	err := streamRelay(ctx, w, cfg, []string{"owner/repo"})
	if err == nil || !strings.Contains(err.Error(), "stream closed") {
		t.Errorf("streamRelay should end with stream closed, got: %v", err)
	}
}

// TestStreamRelay_OversizedLine verifies that a line exceeding the scanner's 1 MiB
// cap surfaces the scan error instead of being silently truncated into bogus events.
func TestStreamRelay_OversizedLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(make([]byte, 1<<20+1)) // one oversized "line" (no newline)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w := &eventWorker{}
	cfg := &cliConfig{PlatformURL: srv.URL, LicenseKey: "test-key"}
	if err := streamRelay(ctx, w, cfg, []string{"owner/repo"}); err == nil {
		t.Error("streamRelay should surface the scanner error for an oversized line")
	}
}

// TestRunRelay_CancelDuringStream covers the path where ctx is cancelled while
// streamRelay is mid-read: the disconnect is recognized as a shutdown (ctx.Err()
// non-nil) and runRelay returns immediately rather than logging a reconnect.
func TestRunRelay_CancelDuringStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done() // hold the stream open until the client goes away
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	w := &eventWorker{}
	cfg := &cliConfig{PlatformURL: srv.URL, LicenseKey: "test-key"}
	done := make(chan struct{})
	go func() {
		runRelay(ctx, w, cfg, []string{"owner/repo"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runRelay did not return after ctx cancel")
	}
}

func TestRunRelay_RefusedReconnects(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	w := &eventWorker{}
	cfg := &cliConfig{PlatformURL: srv.URL, LicenseKey: "test-key"}
	runRelay(ctx, w, cfg, []string{"owner/repo"})

	if calls < 1 {
		t.Errorf("runRelay should have attempted at least one connection, got %d", calls)
	}
}
