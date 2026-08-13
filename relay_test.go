package main

import (
	"os"
	"testing"
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
