package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureClone(t *testing.T) {
	fake := t.TempDir()
	gh := filepath.Join(fake, "gh")
	body := `#!/bin/sh
if [ "$1" = "repo" ] && [ "$2" = "clone" ]; then
	mkdir -p "$4"
	touch "$4/.git"
	echo called > "$0.called"
	exit 0
else
	exit 1
fi
`
	if err := os.WriteFile(gh, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	t.Run("clones when .git is absent", func(t *testing.T) {
		dir := t.TempDir()
		if err := ensureClone(dir, "owner/repo"); err != nil {
			t.Fatalf("ensureClone: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			t.Fatalf("expected .git to exist after clone: %v", err)
		}
		if _, err := os.Stat(gh + ".called"); err == nil {
			os.Remove(gh + ".called")
		}
	})

	t.Run("skips clone when .git already exists", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := ensureClone(dir, "owner/repo"); err != nil {
			t.Fatalf("ensureClone: %v", err)
		}
		if _, err := os.Stat(gh + ".called"); err == nil {
			t.Error("gh repo clone should not be called when .git exists")
		}
	})

	t.Run("returns error when gh clone fails", func(t *testing.T) {
		// Replace the fake gh with one that always fails for this subtest.
		if err := os.WriteFile(gh, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := ensureClone(dir, "owner/repo"); err == nil {
			t.Fatal("expected ensureClone to fail when gh clone fails")
		}
		if _, err := os.Stat(filepath.Join(dir, "marker")); !os.IsNotExist(err) {
			t.Fatalf("expected workspace to be cleared after failed clone, got err=%v", err)
		}
	})
}
