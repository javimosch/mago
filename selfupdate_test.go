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

// TestFileSHA12 verifies the helper returns the first 12 hex chars of a file's
// sha256, and an empty string when the file is missing.
func TestFileSHA12(t *testing.T) {
	dir := t.TempDir()

	// Known SHA-256 for "hello" -> 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := fileSHA12(path); got != "2cf24dba5fb0" {
		t.Errorf("fileSHA12(hello) = %q, want %q", got, "2cf24dba5fb0")
	}

	if got := fileSHA12(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("missing file SHA should be empty, got %q", got)
	}
}
