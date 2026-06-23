package main

import (
	"os"
	"strings"
	"testing"
)

// writeProjects writes a projects.json mapping name -> repo for the test company.
func writeProjects(t *testing.T, c *Company, repos map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("{")
	first := true
	for name, repo := range repos {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString("\"" + name + "\":\"" + repo + "\"")
	}
	b.WriteString("}")
	if err := os.WriteFile(c.projectsConfigFile(), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}
}

// TestBacklogRepoWarning_SilentWhenRepoResolved verifies that no warning is emitted once a
// backlog repo is resolved (GitHub-backed mode is active), regardless of the relay flag.
func TestBacklogRepoWarning_SilentWhenRepoResolved(t *testing.T) {
	c := newTestCompany(t)
	c.ghRepo = "acme/backlog"
	t.Setenv("MAGO_TASK_LABEL", "mago")
	if msg := c.backlogRepoWarning(true); msg != "" {
		t.Errorf("expected no warning when ghRepo is set, got: %q", msg)
	}
	if msg := c.backlogRepoWarning(false); msg != "" {
		t.Errorf("expected no warning when ghRepo is set (no relay), got: %q", msg)
	}
}

// TestBacklogRepoWarning_SilentWhenLocalIntended verifies that a plain local-only worker
// (no repo, no relay, no task label, no project repos) gets no warning — local mode is fine.
func TestBacklogRepoWarning_SilentWhenLocalIntended(t *testing.T) {
	c := newTestCompany(t)
	os.Unsetenv("MAGO_TASK_LABEL")
	if msg := c.backlogRepoWarning(false); msg != "" {
		t.Errorf("expected no warning for an intentionally local worker, got: %q", msg)
	}
}

// TestBacklogRepoWarning_RelayExpectsGitHub verifies that serving via --relay with no backlog
// repo warns and names both fixes (MAGO_GH_REPO and `mago project add`).
func TestBacklogRepoWarning_RelayExpectsGitHub(t *testing.T) {
	c := newTestCompany(t)
	os.Unsetenv("MAGO_TASK_LABEL")
	msg := c.backlogRepoWarning(true)
	if msg == "" {
		t.Fatal("expected a warning when relaying with no backlog repo")
	}
	assertNamesFixes(t, msg)
}

// TestBacklogRepoWarning_TaskLabelExpectsGitHub verifies that setting MAGO_TASK_LABEL (label-
// scoped GitHub backlog) with no repo warns even without relay.
func TestBacklogRepoWarning_TaskLabelExpectsGitHub(t *testing.T) {
	c := newTestCompany(t)
	t.Setenv("MAGO_TASK_LABEL", "mago")
	msg := c.backlogRepoWarning(false)
	if msg == "" {
		t.Fatal("expected a warning when MAGO_TASK_LABEL is set with no backlog repo")
	}
	assertNamesFixes(t, msg)
}

// TestBacklogRepoWarning_AmbiguousProjects verifies that two-plus distinct project repos with
// MAGO_GH_REPO unset warns about the ambiguity and names both fixes — even without relay or label.
func TestBacklogRepoWarning_AmbiguousProjects(t *testing.T) {
	c := newTestCompany(t)
	os.Unsetenv("MAGO_TASK_LABEL")
	writeProjects(t, c, map[string]string{"a": "acme/one", "b": "acme/two"})
	msg := c.backlogRepoWarning(false)
	if msg == "" {
		t.Fatal("expected a warning when multiple project repos are configured with no backlog repo")
	}
	if !strings.Contains(msg, "2 project repos") {
		t.Errorf("ambiguous warning should report the repo count, got: %q", msg)
	}
	assertNamesFixes(t, msg)
}

// assertNamesFixes checks that a warning names both actionable fixes the operator can apply.
func assertNamesFixes(t *testing.T, msg string) {
	t.Helper()
	if !strings.Contains(msg, "MAGO_GH_REPO") {
		t.Errorf("warning should name MAGO_GH_REPO, got: %q", msg)
	}
	if !strings.Contains(msg, "mago project add") {
		t.Errorf("warning should name `mago project add`, got: %q", msg)
	}
	if !strings.HasPrefix(msg, "[warn]") {
		t.Errorf("warning should carry the [warn] prefix, got: %q", msg)
	}
}
