package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGH writes a `gh` stub to a fresh temp bin dir and prepends it to PATH.
func fakeGH(t *testing.T, body string) {
	t.Helper()
	bin := t.TempDir()
	gh := filepath.Join(bin, "gh")
	if err := os.WriteFile(gh, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
}

// TestPrepProjectWorkspaceCloneError verifies that an ensureClone failure (gh repo
// clone exits non-zero) propagates out of prepProjectWorkspace instead of being
// swallowed — the caller needs to know the workspace was never prepared.
func TestPrepProjectWorkspaceCloneError(t *testing.T) {
	fakeGH(t, "#!/bin/sh\nexit 1\n")

	c := &Company{Dir: t.TempDir(), Name: "test"}
	if _, err := c.prepProjectWorkspace(&Task{ID: "1"}, "owner/repo"); err == nil {
		t.Fatal("expected prepProjectWorkspace to fail when the clone fails")
	}
}

// TestPrepProjectWorkspaceFetchDefaultError covers the branch where the task's remote
// branch doesn't exist AND fetching the default branch fails (e.g. the remote has no
// `main`), so prepProjectWorkspace must surface the fetch error.
func TestPrepProjectWorkspaceFetchDefaultError(t *testing.T) {
	bareDir := t.TempDir() // empty remote: no branches at all
	gitRunT(t, "", "init", "-q", "--bare", bareDir)

	fakeGH(t, `#!/bin/sh
if [ "$1" = "repo" ] && [ "$2" = "view" ]; then
	echo "main"
	exit 0
fi
exit 1
`)

	companyDir := t.TempDir()
	ws := filepath.Join(companyDir, "workspace")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing clone (ensureClone no-ops on .git) whose origin lacks any branches.
	gitRunT(t, ws, "init", "-q")
	gitRunT(t, ws, "remote", "add", "origin", bareDir)

	c := &Company{Dir: companyDir, Name: "test"}
	if _, err := c.prepProjectWorkspace(&Task{ID: "1"}, "owner/repo"); err == nil {
		t.Fatal("expected prepProjectWorkspace to fail when the default branch cannot be fetched")
	}
}

// TestPushStateSetupError exercises the pushState early-return when ensureStateRepo
// fails: with MAGO_STATE_SYNC=1 but a company dir that is a plain file, `git init`
// cannot run and the error must be reported (stderr) rather than crash the tick.
func TestPushStateSetupError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Company{Dir: f, Name: "test", ghRepo: "owner/repo"}
	t.Setenv("MAGO_STATE_SYNC", "1")
	c.pushState("sync") // must not panic; error goes to stderr
}

// TestPushDefsAdoptsRemoteMain covers the pushDefs path where the remote already has
// a main branch: local `main` is force-branched from origin/main before the .maindefs
// worktree is attached, so definitions build on the published main rather than a
// fresh empty root commit.
func TestPushDefsAdoptsRemoteMain(t *testing.T) {
	bareDir := seedRemoteMain(t)
	ghRepo := "example/adopts-main-repo"
	redirectGithubRepo(t, ghRepo, bareDir)

	companyDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(companyDir, ".mago", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(companyDir, ".mago", "agents", "cto.md"), []byte("cto\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(companyDir, ".mago", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(companyDir, ".mago", "projects.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Company{Dir: companyDir, Name: "test", ghRepo: ghRepo}
	if err := c.ensureStateRepo(); err != nil {
		t.Fatalf("ensureStateRepo: %v", err)
	}
	c.pushDefs()

	// Definitions must land on top of the remote's existing main (README included).
	tree := gitRunT(t, bareDir, "ls-tree", "-r", "--name-only", "main")
	for _, want := range []string{"README", ".mago/agents/cto.md"} {
		found := false
		for _, line := range strings.Split(tree, "\n") {
			if strings.TrimSpace(line) == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q on remote main, got tree:\n%s", want, tree)
		}
	}
}

// TestPushDefsWorktreeAddError covers the pushDefs early-return when the .maindefs
// worktree cannot be attached (here: a regular file already sits at that path).
func TestPushDefsWorktreeAddError(t *testing.T) {
	companyDir := t.TempDir()
	gitRunT(t, companyDir, "init", "-q")
	gitRunT(t, companyDir, "config", "user.email", "mago@local")
	gitRunT(t, companyDir, "config", "user.name", "mago")

	wt := filepath.Join(companyDir, ".maindefs")
	if err := os.WriteFile(wt, []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Company{Dir: companyDir, Name: "test", ghRepo: "owner/repo"}
	c.pushDefs() // worktree add fails; error goes to stderr, no panic

	if st, err := os.Stat(wt); err != nil || !st.Mode().IsRegular() {
		t.Fatalf("expected .maindefs to remain a regular file, err=%v", err)
	}
}

// TestPushStatePushFailures covers both push-error branches: the runtime push to
// mago-state and the definitions push to main each log to stderr and continue when
// the remote is unreachable (origin resolves to a nonexistent path).
func TestPushStatePushFailures(t *testing.T) {
	ghRepo := "example/unreachable-repo"
	redirectGithubRepo(t, ghRepo, filepath.Join(t.TempDir(), "missing.git"))

	companyDir := t.TempDir()
	for _, p := range []string{".mago/agents", ".mago/skills"} {
		if err := os.MkdirAll(filepath.Join(companyDir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(companyDir, "STATE.md"), []byte("# state\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(companyDir, ".mago", "agents", "cto.md"), []byte("cto\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(companyDir, ".mago", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(companyDir, ".mago", "projects.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Company{Dir: companyDir, Name: "test", ghRepo: ghRepo}
	t.Setenv("MAGO_STATE_SYNC", "1")
	c.pushState("sync") // both pushes fail; errors are logged, not fatal
}
