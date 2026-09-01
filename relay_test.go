package main

import (
	"context"
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
