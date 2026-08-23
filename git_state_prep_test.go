package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// seedRemoteMain creates a bare repo with a main branch holding one commit.
func seedRemoteMain(t *testing.T) string {
	t.Helper()
	bareDir := t.TempDir()
	gitRunT(t, "", "init", "-q", "--bare", bareDir)

	seedDir := t.TempDir()
	gitRunT(t, seedDir, "init", "-q")
	gitRunT(t, seedDir, "config", "user.email", "seed@local")
	gitRunT(t, seedDir, "config", "user.name", "seed")
	if err := os.WriteFile(filepath.Join(seedDir, "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, seedDir, "add", "README")
	gitRunT(t, seedDir, "checkout", "-q", "-b", "main")
	gitRunT(t, seedDir, "commit", "-q", "-m", "init")
	gitRunT(t, seedDir, "remote", "add", "origin", bareDir)
	gitRunT(t, seedDir, "push", "-q", "origin", "main")
	return bareDir
}

func TestPrepProjectWorkspaceFreshBranch(t *testing.T) {
	repoPath := seedRemoteMain(t)

	fake := t.TempDir()
	gh := filepath.Join(fake, "gh")
	body := `#!/bin/sh
if [ "$1" = "repo" ] && [ "$2" = "clone" ]; then
	git clone --depth 1 --single-branch "$3" "$4"
	exit
elif [ "$1" = "repo" ] && [ "$2" = "view" ]; then
	echo "main"
	exit 0
fi
exit 1
`
	if err := os.WriteFile(gh, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	companyDir := t.TempDir()
	c := &Company{Dir: companyDir, Name: "test"}
	task := &Task{ID: "1"}

	ws, err := c.prepProjectWorkspace(task, repoPath)
	if err != nil {
		t.Fatalf("prepProjectWorkspace: %v", err)
	}
	want := filepath.Join(companyDir, "workspace")
	if ws != want {
		t.Fatalf("workspace = %q, want %q", ws, want)
	}
	if _, err := os.Stat(filepath.Join(ws, ".git")); err != nil {
		t.Fatalf("expected .git in workspace: %v", err)
	}

	cmd := exec.Command("git", "-C", ws, "symbolic-ref", "--short", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git symbolic-ref: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "mago/task-1" {
		t.Fatalf("HEAD = %q, want mago/task-1", got)
	}
}
