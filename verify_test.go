package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyEnabled(t *testing.T) {
	os.Unsetenv("MAGO_VERIFY")
	os.Unsetenv("MAGO_VERIFY_CMD")
	if verifyEnabled() {
		t.Error("disabled by default")
	}
	t.Setenv("MAGO_VERIFY", "1")
	if !verifyEnabled() {
		t.Error("MAGO_VERIFY=1 should enable")
	}
	os.Unsetenv("MAGO_VERIFY")
	t.Setenv("MAGO_VERIFY_CMD", "make test")
	if !verifyEnabled() {
		t.Error("MAGO_VERIFY_CMD should enable")
	}
}

func TestVerifyCommand(t *testing.T) {
	dir := t.TempDir()

	// explicit command wins
	t.Setenv("MAGO_VERIFY_CMD", "npm test")
	if cmd, _ := verifyCommand(dir); cmd != "npm test" {
		t.Errorf("MAGO_VERIFY_CMD not honored, got %q", cmd)
	}

	// auto-detect Go when no explicit command
	os.Unsetenv("MAGO_VERIFY_CMD")
	if cmd, _ := verifyCommand(dir); cmd != "" {
		t.Errorf("no go.mod -> no command, got %q", cmd)
	}
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644)
	if cmd, label := verifyCommand(dir); cmd == "" || label != "go build + test" {
		t.Errorf("go.mod should detect go build+test, got %q/%q", cmd, label)
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\n", 2); got != "c\nd" {
		t.Errorf("lastLines = %q, want %q", got, "c\nd")
	}
	if got := lastLines("only", 5); got != "only" {
		t.Errorf("lastLines short = %q", got)
	}
}

func TestRunShell(t *testing.T) {
	if out, err := runShell(t.TempDir(), "echo hi", 0); err == nil {
		_ = out // 0 timeout cancels immediately; just ensure it doesn't panic
	}
	out, err := runShell(t.TempDir(), "echo hello", 10_000_000_000)
	if err != nil || out == "" {
		t.Errorf("runShell echo failed: %v %q", err, out)
	}
}
