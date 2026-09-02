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

func TestVerifyCommand_WhitespaceOnly(t *testing.T) {
	dir := t.TempDir()

	// A whitespace-only custom command should be treated as unset.
	t.Setenv("MAGO_VERIFY_CMD", "   \t  ")
	if cmd, _ := verifyCommand(dir); cmd != "" {
		t.Errorf("whitespace-only MAGO_VERIFY_CMD should be ignored, got %q", cmd)
	}

	// After it is ignored, go.mod auto-detection should still kick in.
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644)
	if cmd, label := verifyCommand(dir); cmd == "" || label != "go build + test" {
		t.Errorf("go.mod should still be detected, got %q/%q", cmd, label)
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\n", 2); got != "c\nd" {
		t.Errorf("lastLines = %q, want %q", got, "c\nd")
	}
	if got := lastLines("only", 5); got != "only" {
		t.Errorf("lastLines short = %q", got)
	}
	if got := lastLines("\na\n\nb\n\n", 1); got != "b" {
		t.Errorf("lastLines skip blanks = %q, want %q", got, "b")
	}
	if got := lastLines("   \n\t\n", 3); got != "" {
		t.Errorf("lastLines only blanks = %q, want empty", got)
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

func TestVerificationPath(t *testing.T) {
	cases := []struct {
		home, path, want string
	}{
		{"/root", "/bin:/usr/bin", "/usr/local/go/bin:/root/go/bin:/bin:/usr/bin"},
		{"  /root  ", "  /bin  ", "/usr/local/go/bin:/root/go/bin:/bin"},
		{"  /home/user  ", "  /opt/bin:/opt/sbin  ", "/usr/local/go/bin:/home/user/go/bin:/opt/bin:/opt/sbin"},
		{"", "", "/usr/local/go/bin:" + filepath.Join("", "go", "bin") + ":"},
	}
	for _, c := range cases {
		got := verificationPath(c.home, c.path)
		if got != c.want {
			t.Errorf("verificationPath(%q, %q) = %q, want %q", c.home, c.path, got, c.want)
		}
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

// TestVerifyPR_NoChecksDetected covers the successful fetch/checkout path in verifyPR when
// the checkout has no recognizable project file and no MAGO_VERIFY_CMD is configured.
func TestVerifyPR_NoChecksDetected(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".mago"), 0o755); err != nil {
		t.Fatalf("mkdir .mago: %v", err)
	}

	// Build a bare "remote" with a pull/1/head ref pointing at a tree with no go.mod.
	bareDir := t.TempDir()
	gitRunT(t, "", "init", "-q", "--bare", bareDir)

	seedDir := t.TempDir()
	gitRunT(t, seedDir, "init", "-q")
	gitRunT(t, seedDir, "config", "user.email", "seed@local")
	gitRunT(t, seedDir, "config", "user.name", "seed")
	gitRunT(t, seedDir, "checkout", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(seedDir, "README"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, seedDir, "add", "README")
	gitRunT(t, seedDir, "commit", "-q", "-m", "seed")
	gitRunT(t, seedDir, "remote", "add", "origin", bareDir)
	gitRunT(t, seedDir, "push", "-q", "origin", "main")

	sha := strings.TrimSpace(gitRunT(t, seedDir, "rev-parse", "HEAD"))
	gitRunT(t, "", "--git-dir", bareDir, "update-ref", "refs/pull/1/head", sha)

	// Pre-populate the verify clone so ensureClone is a no-op.
	verifyDir := filepath.Join(dir, ".mago", "verify", "owner-repo")
	gitRunT(t, "", "clone", "-q", bareDir, verifyDir)

	t.Setenv("MAGO_VERIFY_CMD", "")
	c := &Company{Dir: dir, Name: "t"}
	c.saveMode(workerMode{Merge: "verified"})

	res := c.verifyPR("owner/repo", 1)
	if res.ran || res.ok || !strings.Contains(res.detail, "no automated checks detected") {
		t.Errorf("verifyPR no checks = %+v, want no-run with 'no automated checks detected'", res)
	}
}
