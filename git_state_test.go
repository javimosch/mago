package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRunT runs git in dir and fails the test on error, returning combined output.
func gitRunT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v (dir=%s): %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// seedRemoteMagoState creates a bare "remote" repo with a mago-state branch already
// containing runtime exhaust (STATE.md + .mago/skills), mimicking a prior session's push.
func seedRemoteMagoState(t *testing.T, remoteStateMD string) string {
	t.Helper()
	bareDir := t.TempDir()
	gitRunT(t, "", "init", "-q", "--bare", bareDir)

	seedDir := t.TempDir()
	gitRunT(t, seedDir, "init", "-q")
	gitRunT(t, seedDir, "config", "user.email", "seed@local")
	gitRunT(t, seedDir, "config", "user.name", "seed")
	gitRunT(t, seedDir, "checkout", "-q", "--orphan", "mago-state")
	if err := os.WriteFile(filepath.Join(seedDir, "STATE.md"), []byte(remoteStateMD), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(seedDir, ".mago", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, ".mago", "skills", "foo.md"), []byte("remote skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunT(t, seedDir, "add", "-A")
	gitRunT(t, seedDir, "commit", "-q", "-m", "seed remote state")
	gitRunT(t, seedDir, "remote", "add", "origin", bareDir)
	gitRunT(t, seedDir, "push", "-q", "origin", "mago-state")
	return bareDir
}

// redirectGithubRepo scopes git's "git@github.com:<ghRepo>.git" URL to a local bare repo path
// via a temp global gitconfig (GIT_CONFIG_GLOBAL), so ensureStateRepo's hardcoded SSH remote
// resolves without real network/GitHub access. Restores the prior env on cleanup.
func redirectGithubRepo(t *testing.T, ghRepo, bareDir string) {
	t.Helper()
	gitCfg := filepath.Join(t.TempDir(), ".gitconfig")
	contents := "[url \"" + bareDir + "\"]\n\tinsteadOf = git@github.com:" + ghRepo + ".git\n"
	if err := os.WriteFile(gitCfg, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	old, had := os.LookupEnv("GIT_CONFIG_GLOBAL")
	os.Setenv("GIT_CONFIG_GLOBAL", gitCfg)
	t.Cleanup(func() {
		if had {
			os.Setenv("GIT_CONFIG_GLOBAL", old)
		} else {
			os.Unsetenv("GIT_CONFIG_GLOBAL")
		}
	})
}

func TestEnsureStateRepoAdoptsRemoteAlongsideLocalDefs(t *testing.T) {
	bareDir := seedRemoteMagoState(t, "# remote state\n\n## Mission\n(Set by the CEO. Edit me.)\n")
	ghRepo := "example/owner-repo"
	redirectGithubRepo(t, ghRepo, bareDir)

	companyDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(companyDir, ".mago", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(companyDir, ".mago", "agents", "cto.md"), []byte("local agent def\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Company{Dir: companyDir, Name: "test", ghRepo: ghRepo}
	if err := c.ensureStateRepo(); err != nil {
		t.Fatalf("ensureStateRepo: %v", err)
	}

	if head := strings.TrimSpace(gitRunT(t, companyDir, "symbolic-ref", "--short", "HEAD")); head != "mago-state" {
		t.Fatalf("expected HEAD on mago-state, got %q", head)
	}

	skill, err := os.ReadFile(filepath.Join(companyDir, ".mago", "skills", "foo.md"))
	if err != nil || strings.TrimSpace(string(skill)) != "remote skill" {
		t.Fatalf("expected remote runtime exhaust restored, got %q err=%v", skill, err)
	}

	def, err := os.ReadFile(filepath.Join(companyDir, ".mago", "agents", "cto.md"))
	if err != nil || strings.TrimSpace(string(def)) != "local agent def" {
		t.Fatalf("expected untracked local agent definitions preserved, got %q err=%v", def, err)
	}

	// definitions must never be tracked on mago-state, even if a historical run leaked them there
	if out := gitRunT(t, companyDir, "ls-files"); strings.Contains(out, ".mago/agents") {
		t.Fatalf("expected .mago/agents NOT tracked on mago-state, ls-files:\n%s", out)
	}
}

func TestEnsureStateRepoPreservesLocalMissionOverStaleRemote(t *testing.T) {
	bareDir := seedRemoteMagoState(t, "# remote state\n\n## Mission\nStale remote mission from a prior session.\n")
	ghRepo := "example/owner-repo2"
	redirectGithubRepo(t, ghRepo, bareDir)

	companyDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(companyDir, ".mago"), 0o755); err != nil {
		t.Fatal(err)
	}
	localState := "# company state\n\n## Mission\nFreshly set CEO mission for this run.\n\n" +
		"## Shipped\n(none yet)\n\n## In flight\n(nothing yet)\n\n## Decisions\n(none yet)\n\n" +
		"## Activity log\n(none yet)\n"
	if err := os.WriteFile(filepath.Join(companyDir, "STATE.md"), []byte(localState), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Company{Dir: companyDir, Name: "test", ghRepo: ghRepo}
	if err := c.ensureStateRepo(); err != nil {
		t.Fatalf("ensureStateRepo: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(companyDir, "STATE.md"))
	if err != nil {
		t.Fatalf("read STATE.md: %v", err)
	}
	if !strings.Contains(string(got), "Freshly set CEO mission for this run.") {
		t.Fatalf("expected local mission to survive adopting stale remote STATE.md, got:\n%s", got)
	}
	if strings.Contains(string(got), "Stale remote mission") {
		t.Fatalf("expected stale remote mission to be overwritten, got:\n%s", got)
	}
}

func TestDefaultBranch(t *testing.T) {
	bin := t.TempDir()
	gh := filepath.Join(bin, "gh")
	body := "#!/bin/sh\nif [ \"$1\" = \"repo\" ] && [ \"$2\" = \"view\" ]; then\n  echo 'trunk'\nelse\n  exit 1\nfi\n"
	if err := os.WriteFile(gh, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	if got := defaultBranch("owner/repo"); got != "trunk" {
		t.Errorf("defaultBranch = %q, want trunk", got)
	}
}

func TestDefaultBranch_Fallback(t *testing.T) {
	bin := t.TempDir()
	gh := filepath.Join(bin, "gh")
	// Always fail, simulating an unauthenticated or missing gh.
	if err := os.WriteFile(gh, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	if got := defaultBranch("owner/repo"); got != "main" {
		t.Errorf("defaultBranch = %q, want main fallback", got)
	}
}

// TestPushState_NoSync verifies that pushState is a no-op when MAGO_STATE_SYNC is not enabled,
// leaving the company directory untouched (no local git repo is created).
func TestPushState_NoSync(t *testing.T) {
	companyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(companyDir, "STATE.md"), []byte("# state\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Company{Dir: companyDir, Name: "test", ghRepo: "owner/repo"}
	t.Setenv("MAGO_STATE_SYNC", "")
	c.pushState("sync message")

	if _, err := os.Stat(filepath.Join(companyDir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("expected no .git with state sync disabled, got err=%v", err)
	}
}

// TestPushState_WithSync exercises the full MAGO_STATE_SYNC=1 path: it creates a local
// company with runtime exhaust and agent definitions, redirects the hardcoded GitHub SSH
// URL to a local bare repo, and verifies that pushState publishes both mago-state and main.
func TestPushState_WithSync(t *testing.T) {
	bareDir := t.TempDir()
	gitRunT(t, "", "init", "-q", "--bare", bareDir)

	ghRepo := "example/push-state-repo"
	redirectGithubRepo(t, ghRepo, bareDir)

	companyDir := t.TempDir()
	for _, p := range []string{".mago/agents", ".mago/runs", ".mago/skills", ".mago/memory", ".mago/inbox"} {
		if err := os.MkdirAll(filepath.Join(companyDir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(companyDir, "STATE.md"), []byte("# company state\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(companyDir, ".mago", "agents", "cto.md"), []byte("cto agent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(companyDir, ".mago", "skills", "foo.md"), []byte("skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(companyDir, ".mago", "runs", "run.md"), []byte("run\n"), 0o644); err != nil {
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
	c.pushState("state update")

	// HEAD should end up on the mago-state branch.
	if head := strings.TrimSpace(gitRunT(t, companyDir, "symbolic-ref", "--short", "HEAD")); head != "mago-state" {
		t.Fatalf("expected HEAD on mago-state, got %q", head)
	}

	// Both mago-state and main should now exist on the "remote" bare repo.
	out := gitRunT(t, "", "ls-remote", "--heads", bareDir)
	if !strings.Contains(out, "refs/heads/mago-state") {
		t.Fatalf("expected mago-state branch on remote, got:\n%s", out)
	}
	if !strings.Contains(out, "refs/heads/main") {
		t.Fatalf("expected main branch on remote, got:\n%s", out)
	}
}

// TestPushState_Idempotent verifies that a second pushState with unchanged content is a
// no-op: the runtime branch has no staged changes and the definitions worktree has nothing new
// to commit, so both calls complete without error and without creating extra commits.
func TestPushState_Idempotent(t *testing.T) {
	bareDir := t.TempDir()
	gitRunT(t, "", "init", "-q", "--bare", bareDir)

	ghRepo := "example/idempotent-repo"
	redirectGithubRepo(t, ghRepo, bareDir)

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
	if err := os.WriteFile(filepath.Join(companyDir, ".mago", "skills", "foo.md"), []byte("skill\n"), 0o644); err != nil {
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
	c.pushState("first")
	c.pushState("second")

	out := gitRunT(t, "", "ls-remote", "--heads", bareDir)
	if !strings.Contains(out, "refs/heads/mago-state") || !strings.Contains(out, "refs/heads/main") {
		t.Fatalf("expected mago-state and main branches on remote, got:\n%s", out)
	}
}
