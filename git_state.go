package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ensureClone makes dir a clone of the given GitHub repo (clone if absent). gh handles
// auth; the agent then branches/commits/pushes/PRs inside this working tree via bash.
func ensureClone(dir, repo string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	os.RemoveAll(dir)
	out, err := exec.Command("gh", "repo", "clone", repo, dir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("clone %s: %v %s", repo, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// pushState commits the company's runtime state (STATE.md + .mago, excluding product
// workspaces and local task files) to the mago-state branch of the GitHub repo and
// pushes. No-op unless the company is in GitHub mode. This is the "your data lives in
// your own GitHub" closure for orchestration state.
func (c *Company) pushState(msg string) {
	if c.ghRepo == "" {
		return
	}
	if err := c.ensureStateRepo(); err != nil {
		fmt.Fprintf(os.Stderr, "[state] setup: %v\n", err)
		return
	}
	gitRun(c.Dir, "add", "-A")
	if out, _ := gitRun(c.Dir, "status", "--porcelain"); strings.TrimSpace(out) == "" {
		return // nothing changed
	}
	if out, err := gitRun(c.Dir, "commit", "-q", "-m", msg); err != nil {
		fmt.Fprintf(os.Stderr, "[state] commit: %v %s\n", err, out)
		return
	}
	if out, err := gitRun(c.Dir, "push", "-q", "origin", "mago-state"); err != nil {
		fmt.Fprintf(os.Stderr, "[state] push: %v %s\n", err, out)
		return
	}
	fmt.Fprintf(os.Stderr, "[state] pushed %s @ mago-state\n", c.ghRepo)
}

func (c *Company) ensureStateRepo() error {
	if _, err := os.Stat(filepath.Join(c.Dir, ".git")); err == nil {
		return nil
	}
	// product code and local task files do not belong on the orchestration branch
	os.WriteFile(filepath.Join(c.Dir, ".gitignore"), []byte("workspace/\nprojects/\ntasks/\n"), 0o644)
	if out, err := gitRun(c.Dir, "init", "-q"); err != nil {
		return fmt.Errorf("init: %v %s", err, out)
	}
	gitRun(c.Dir, "config", "user.email", "mago@local")
	gitRun(c.Dir, "config", "user.name", "mago")
	gitRun(c.Dir, "remote", "add", "origin", "git@github.com:"+c.ghRepo+".git")
	if out, err := gitRun(c.Dir, "checkout", "-q", "--orphan", "mago-state"); err != nil {
		return fmt.Errorf("orphan branch: %v %s", err, out)
	}
	return nil
}
