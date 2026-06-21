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

func gitOK(dir string, args ...string) bool {
	_, err := gitRun(dir, args...)
	return err == nil
}

// emptyTreeSHA is git's well-known empty tree object (identical in every repo) — used to
// create a fresh root commit for `main` without a working-tree checkout.
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// stateGitignore keeps product code, local task files, agent DEFINITIONS (which live on
// main), and the defs worktree OUT of the mago-state runtime branch.
const stateGitignore = "workspace/\nprojects/\ntasks/\n.mago/agents/\n.mago/config.json\n.mago/projects.json\n.maindefs/\n"

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

// pushState publishes the company's state in GitHub mode: runtime exhaust (STATE.md +
// .mago/runs|skills|memory|inbox) to the mago-state branch, and agent DEFINITIONS
// (.mago/agents + config + projects) to main. No-op outside GitHub mode.
func (c *Company) pushState(msg string) {
	if c.ghRepo == "" {
		return
	}
	if err := c.ensureStateRepo(); err != nil {
		fmt.Fprintf(os.Stderr, "[state] setup: %v\n", err)
		return
	}
	// runtime -> mago-state: stage ONLY known runtime exhaust. Never `git add -A` — that
	// would sweep up stray files (logs, scratch) sitting in the company dir into the
	// runtime branch, polluting it and causing re-clone conflicts.
	var paths []string
	for _, p := range []string{"STATE.md", ".mago/runs", ".mago/skills", ".mago/memory", ".mago/inbox"} {
		if _, err := os.Stat(filepath.Join(c.Dir, p)); err == nil {
			paths = append(paths, p)
		}
	}
	if len(paths) > 0 {
		gitRun(c.Dir, append([]string{"add", "--"}, paths...)...)
		if !gitOK(c.Dir, "diff", "--cached", "--quiet") { // there are staged changes
			if _, err := gitRun(c.Dir, "commit", "-q", "-m", msg); err == nil {
				if out, err := gitRun(c.Dir, "push", "-q", "origin", "mago-state"); err != nil {
					fmt.Fprintf(os.Stderr, "[state] push mago-state: %v %s\n", err, out)
				} else {
					fmt.Fprintf(os.Stderr, "[state] runtime -> %s @ mago-state\n", c.ghRepo)
				}
			}
		}
	}
	// definitions -> main
	c.pushDefs()
}

func (c *Company) ensureStateRepo() error {
	if _, err := os.Stat(filepath.Join(c.Dir, ".git")); err == nil {
		return nil
	}
	if out, err := gitRun(c.Dir, "init", "-q"); err != nil {
		return fmt.Errorf("init: %v %s", err, out)
	}
	gitRun(c.Dir, "config", "user.email", "mago@local")
	gitRun(c.Dir, "config", "user.name", "mago")
	gitRun(c.Dir, "remote", "add", "origin", "git@github.com:"+c.ghRepo+".git")
	gitRun(c.Dir, "fetch", "-q", "origin")

	if gitOK(c.Dir, "rev-parse", "--verify", "origin/mago-state") {
		// Adopt the remote runtime history so pushes fast-forward (re-clone safe).
		// Remove fresh local files that would conflict with the remote checkout — the
		// remote runtime (incl. its .gitignore) is authoritative.
		os.Remove(filepath.Join(c.Dir, ".gitignore"))
		os.Remove(filepath.Join(c.Dir, "STATE.md"))
		for _, d := range []string{"runs", "skills", "memory", "inbox"} {
			os.RemoveAll(filepath.Join(c.Dir, ".mago", d))
		}
		if out, err := gitRun(c.Dir, "checkout", "-q", "-B", "mago-state", "origin/mago-state"); err != nil {
			return fmt.Errorf("adopt mago-state: %v %s", err, out)
		}
	} else {
		os.WriteFile(filepath.Join(c.Dir, ".gitignore"), []byte(stateGitignore), 0o644)
		if out, err := gitRun(c.Dir, "checkout", "-q", "--orphan", "mago-state"); err != nil {
			return fmt.Errorf("orphan mago-state: %v %s", err, out)
		}
	}
	return nil
}

// pushDefs publishes agent definitions to main via a dedicated worktree, so they are
// human-curated on main rather than buried in the runtime branch.
func (c *Company) pushDefs() {
	wt := filepath.Join(c.Dir, ".maindefs")
	if _, err := os.Stat(filepath.Join(wt, ".git")); err != nil {
		// First time: ensure a local `main` branch (based on origin/main, else a fresh
		// empty root commit) then attach the worktree.
		switch {
		case gitOK(c.Dir, "rev-parse", "--verify", "origin/main"):
			gitRun(c.Dir, "branch", "-f", "main", "origin/main")
		case !gitOK(c.Dir, "rev-parse", "--verify", "main"):
			out, err := gitRun(c.Dir, "commit-tree", emptyTreeSHA, "-m", "mago: init main")
			if err != nil {
				fmt.Fprintf(os.Stderr, "[state] init main: %v %s\n", err, out)
				return
			}
			gitRun(c.Dir, "branch", "main", strings.TrimSpace(out))
		}
		if out, err := gitRun(c.Dir, "worktree", "add", "-q", wt, "main"); err != nil {
			fmt.Fprintf(os.Stderr, "[state] defs worktree: %v %s\n", err, out)
			return
		}
	}
	ensureDir(filepath.Join(wt, ".mago"))
	copyTree(c.agentsDir(), filepath.Join(wt, ".mago", "agents"))
	copyFile(c.projectsConfigFile(), filepath.Join(wt, ".mago", "projects.json"))
	copyFile(filepath.Join(c.magoDir(), "config.json"), filepath.Join(wt, ".mago", "config.json"))
	gitRun(wt, "add", "-A")
	if out, _ := gitRun(wt, "status", "--porcelain"); strings.TrimSpace(out) == "" {
		return
	}
	gitRun(wt, "commit", "-q", "-m", "mago: update agent definitions")
	if out, err := gitRun(wt, "push", "-q", "origin", "main"); err != nil {
		fmt.Fprintf(os.Stderr, "[state] push main: %v %s\n", err, out)
	} else {
		fmt.Fprintf(os.Stderr, "[state] defs -> %s @ main\n", c.ghRepo)
	}
}
