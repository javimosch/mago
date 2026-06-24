package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeBinary(t *testing.T) {
	dir := t.TempDir()

	// A runnable program that prints output for `version` passes the probe.
	good := filepath.Join(dir, "good")
	if err := os.WriteFile(good, []byte("#!/bin/sh\necho 0.0.1-poc\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := probeBinary(good); err != nil {
		t.Errorf("runnable binary should pass the probe, got: %v", err)
	}

	// A truncated/corrupt binary (the rbm21 brick scenario) must be rejected — it won't exec.
	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, []byte("\x7fELF\x00truncated-garbage-not-a-real-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := probeBinary(bad); err == nil {
		t.Error("corrupt binary must be rejected by the probe")
	}

	// A binary that runs but prints nothing is rejected (no usable version output).
	silent := filepath.Join(dir, "silent")
	os.WriteFile(silent, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	if err := probeBinary(silent); err == nil {
		t.Error("empty-output binary must be rejected")
	}

	// A missing path is rejected.
	if err := probeBinary(filepath.Join(dir, "nope")); err == nil {
		t.Error("missing binary must be rejected")
	}
}
