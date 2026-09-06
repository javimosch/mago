package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestStreamRelay_BadURL verifies that an unparseable platform URL fails at
// request construction rather than reaching the dial.
func TestStreamRelay_BadURL(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	cfg := &cliConfig{PlatformURL: "http://%zz", LicenseKey: "test-key"}
	err := streamRelay(ctx, &eventWorker{}, cfg, []string{"owner/repo"})
	if err == nil {
		t.Fatal("streamRelay with an unparseable platform URL should error")
	}
}

// TestStreamRelay_ScannerError verifies that a response body that fails mid-read
// (here: declared Content-Length larger than what the server sends before
// closing) surfaces the scanner's error rather than a generic "stream closed".
func TestStreamRelay_ScannerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("test server cannot hijack the connection")
			return
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		defer conn.Close()
		// Promise 100 body bytes, deliver 5, then close — the client hits an
		// unexpected EOF while scanning the stream.
		if _, err := rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nshort"); err != nil {
			t.Errorf("write response: %v", err)
			return
		}
		rw.Flush()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &cliConfig{PlatformURL: srv.URL, LicenseKey: "test-key"}
	err := streamRelay(ctx, &eventWorker{}, cfg, []string{"owner/repo"})
	if err == nil {
		t.Fatal("streamRelay should surface the truncated-body read error")
	}
	if strings.Contains(err.Error(), "stream closed") {
		t.Errorf("expected the underlying read error, not a clean close, got: %v", err)
	}
}

// TestRunRelay_CancelDuringBackoff verifies that cancelling the context while
// runRelay is sleeping between reconnect attempts exits promptly instead of
// waiting out the backoff timer.
func TestRunRelay_CancelDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Port 1 refuses instantly, so streamRelay fails fast and runRelay enters
	// the reconnect backoff — which the cancel interrupts.
	cfg := &cliConfig{PlatformURL: "http://127.0.0.1:1", LicenseKey: "test-key"}
	time.AfterFunc(50*time.Millisecond, cancel)

	done := make(chan struct{})
	go func() {
		runRelay(ctx, &eventWorker{}, cfg, []string{"owner/repo"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runRelay did not return promptly after ctx cancel during backoff")
	}
}
