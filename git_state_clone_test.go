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
}
