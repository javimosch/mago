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

// stateRuntimePaths is the company's runtime exhaust that lives on the mago-state branch.
var stateRuntimePaths = []string{"STATE.md", ".mago/runs", ".mago/skills", ".mago/memory", ".mago/inbox"}

// ensureClone makes dir a clone of the given GitHub repo (clone if absent). gh handles
// auth; the agent then branches/commits/pushes/PRs inside this working tree via bash.
func ensureClone(dir, repo string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	os.RemoveAll(dir)
	// shallow + single-branch keeps large repos (e.g. supercli's thousands of plugins) fast
	// to clone; branches still push and PRs still open from a shallow base.
	out, err := exec.Command("gh", "repo", "clone", repo, dir, "--", "--depth", "1", "--single-branch").CombinedOutput()
	if err != nil {
		return fmt.Errorf("clone %s: %v %s", repo, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// defaultBranch returns the repo's default branch (main/master), via gh.
func defaultBranch(repo string) string {
	if out, err := gh("repo", "view", repo, "--json", "defaultBranchRef", "--jq", ".defaultBranchRef.name"); err == nil {
		if b := strings.TrimSpace(out); b != "" {
			return b
		}
	}
	return "main"
}

// prepProjectWorkspace makes the project clone current and checks out the task's branch
// before the agent runs, so work never starts from a stale repo state:
//   - resume the agent's existing remote branch if it already pushed one, OR
//   - create the branch fresh from the LATEST origin/<default>.
//
// The agent then just edits, commits, pushes, and opens/updates the PR.
func (c *Company) prepProjectWorkspace(t *Task, repo string) (string, error) {
	ws := c.projectDir(t.Project)
	if err := ensureClone(ws, repo); err != nil {
		return "", err
	}
	branch := "mago/task-" + t.ID
	if _, err := gitRun(ws, "fetch", "-q", "--depth", "1", "origin", branch); err == nil {
		gitRun(ws, "checkout", "-q", "-B", branch, "FETCH_HEAD") // resume existing PR branch
	} else {
		def := defaultBranch(repo)
		if _, err := gitRun(ws, "fetch", "-q", "--depth", "1", "origin", def); err != nil {
			return "", fmt.Errorf("fetch %s/%s: %v", repo, def, err)
		}
		gitRun(ws, "checkout", "-q", "-B", branch, "FETCH_HEAD") // fresh from latest default
	}
	return ws, nil
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
	for _, p := range stateRuntimePaths {
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
		// Adopt the remote runtime history WITHOUT a full `checkout`: the company dir holds live
		// definitions (.mago/agents, projects.json) that checkout would refuse to clobber (and
		// that ticks need). Point branch + index at the remote, then restore ONLY the runtime
		// exhaust into the working tree so it keeps ACCUMULATING; leave definitions untouched.
		gitRun(c.Dir, "branch", "-f", "mago-state", "origin/mago-state")
		if out, err := gitRun(c.Dir, "symbolic-ref", "HEAD", "refs/heads/mago-state"); err != nil {
			return fmt.Errorf("adopt mago-state: %v %s", err, out)
		}
		gitRun(c.Dir, "reset", "-q") // index <- remote tree; working tree kept (defs safe)
		for _, p := range stateRuntimePaths {
			gitRun(c.Dir, "checkout", "-q", "--", p) // restore accumulated exhaust (no-op if absent)
		}
		// Definitions belong on main; drop any a historical run leaked onto mago-state so it
		// converges to runtime-only and stops colliding with the company dir's live defs.
		gitRun(c.Dir, "rm", "-r", "-q", "--cached", "--ignore-unmatch", ".mago/agents", ".mago/config.json", ".mago/projects.json")
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
