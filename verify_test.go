package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestVerifyPR_Disabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".mago"), 0o755); err != nil {
		t.Fatalf("mkdir .mago: %v", err)
	}
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_VERIFY_CMD", "")
	c := &Company{Dir: dir, Name: "t"}

	res := c.verifyPR("owner/repo", 1)
	if res.ran || res.ok || res.detail != "" {
		t.Errorf("verifyPR disabled = %+v, want zero verifyResult", res)
	}
}

// TestVerifyPR_FetchFailure verifies that verifyPR reports a clear detail when the
// PR fetch fails after the clone directory is already present.
func TestVerifyPR_FetchFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".mago"), 0o755); err != nil {
		t.Fatalf("mkdir .mago: %v", err)
	}

	// Pre-seed the verify clone with a .git dir so ensureClone is a no-op, then give it a
	// bogus remote so the fetch of pull/1/head fails.
	verifyDir := filepath.Join(dir, ".mago", "verify", "owner-repo")
	if err := os.MkdirAll(verifyDir, 0o755); err != nil {
		t.Fatalf("mkdir verify dir: %v", err)
	}
	gitRunT(t, verifyDir, "init", "-q")
	gitRunT(t, verifyDir, "remote", "add", "origin", "http://localhost/no-such-repo")

	t.Setenv("MAGO_VERIFY_CMD", "true")
	c := &Company{Dir: dir, Name: "t"}
	res := c.verifyPR("owner/repo", 1)
	if res.ran || res.ok || !strings.Contains(res.detail, "could not fetch PR for verification") {
		t.Errorf("verifyPR fetch failure = %+v, want a fetch-failure detail", res)
	}
}

func TestVerifyEnabled(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "t"}

	// Default mode is "on" (auto-merge) -> verification disabled.
	if c.verifyEnabled() {
		t.Fatal("verifyEnabled should be off in default on/merge mode")
	}

	// verified mode enables verification.
	c.saveMode(workerMode{Merge: "verified"})
	if !c.verifyEnabled() {
		t.Fatal("verifyEnabled should be on in verified merge mode")
	}

	// legacy MAGO_VERIFY_CMD also enables verification regardless of merge mode.
	c.saveMode(workerMode{Merge: "on"})
	t.Setenv("MAGO_VERIFY_CMD", "npm test")
	if !c.verifyEnabled() {
		t.Fatal("verifyEnabled should be on when MAGO_VERIFY_CMD is set")
	}
}
