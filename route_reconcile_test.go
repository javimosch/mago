package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReconcileOnce_ResumesInProgressTask verifies that reconcileOnce skips an
// already-claimed in-progress task in the routing loop, then runTick resumes it.
// MAGO_TEST_BAD_REFLECTION forces a self-heal path so no real model is needed.
func TestReconcileOnce_ResumesInProgressTask(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_PROVIDER", "")
	t.Setenv("MAGO_MODEL", "")
	t.Setenv("MAGO_TEST_BAD_REFLECTION", "2")

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "cto", "---\nname: cto\ntitle: CTO\nimplements: true\n---\n")

	task, err := c.tasks.AddTask("Continue the work", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := c.tasks.Claim(task, "cto"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	worked, err := reconcileOnce(c)
	if err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}
	if !worked {
		t.Error("reconcileOnce should report worked=true when resuming an in-progress task")
	}

	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(ts) != 1 {
		t.Fatalf("expected 1 task, got %d", len(ts))
	}
	if ts[0].Status != "in_progress" {
		t.Errorf("task status = %q, want in_progress", ts[0].Status)
	}
	if ts[0].Assignee != "cto" {
		t.Errorf("task assignee = %q, want cto", ts[0].Assignee)
	}
	if !strings.Contains(ts[0].Body, "Progress log") {
		t.Errorf("task body should still contain the progress log, got:\n%s", ts[0].Body)
	}
}

// assignFailBackend wraps the local backend with an Assign that always fails, to
// exercise reconcileOnce's "assign failed -> keep routing" continue branch.
type assignFailBackend struct{ *localBackend }

func (b assignFailBackend) Assign(t *Task, agent string) error {
	return fmt.Errorf("assign boom")
}

// TestReconcileOnce_BadAgentFileSkipped verifies that an agent definition which
// fails to load (invalid frontmatter) is skipped — not fatal — so reconcile can
// still proceed with the rest of the roster (or, here, an empty one).
func TestReconcileOnce_BadAgentFileSkipped(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "broken", "---\nname: broken\nreviews: yes\n---\n") // invalid bool

	worked, err := reconcileOnce(c)
	if err != nil {
		t.Fatalf("a broken agent file should not fail reconcile, got: %v", err)
	}
	if worked {
		t.Error("no usable agents and no tasks should report worked=false")
	}
}

// TestReconcileOnce_PRCapHoldsTask verifies the open-PR backpressure branch: when
// a task's project repo is already at its mago-PR cap, routing leaves the task
// open and unassigned ("holding") instead of starting more work on that repo.
func TestReconcileOnce_PRCapHoldsTask(t *testing.T) {
	bindir := t.TempDir()
	// `gh pr list --json headRefName` -> one open mago/ PR plus a human one.
	fakeGH := "#!/bin/sh\necho '[{\"headRefName\":\"mago/task-9\"},{\"headRefName\":\"fix/typo\"}]'\n"
	if err := os.WriteFile(filepath.Join(bindir, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_PR_CAP", "1")

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "broken", "---\nname: broken\nreviews: yes\n---\n") // skipped -> empty roster
	if err := os.WriteFile(c.projectsConfigFile(), []byte(`{"web":"acme/web"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.tasks.AddTask("Ship the feature", "web"); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	var worked bool
	var err error
	out := captureStderr(t, func() {
		worked, err = reconcileOnce(c)
	})
	if err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}
	if worked {
		t.Error("no runnable agents should report worked=false")
	}
	if !strings.Contains(out, "at PR cap") {
		t.Errorf("stderr should log the PR-cap hold, got:\n%s", out)
	}

	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if ts[0].Status != "open" || ts[0].Assignee != "" {
		t.Errorf("held task should stay open and unassigned, got status=%q assignee=%q", ts[0].Status, ts[0].Assignee)
	}
}

// TestReconcileOnce_AssignFailureContinues verifies that a routing decision whose
// Assign call fails is logged and skipped — the reconcile continues instead of
// aborting the whole pass.
func TestReconcileOnce_AssignFailureContinues(t *testing.T) {
	bindir := t.TempDir()
	// tauComplete returns the last JSON object with a "content" field -> routes to cto.
	fakeTau := "#!/bin/sh\nprintf '%s\\n' '{\"content\":\"cto\"}'\n"
	if err := os.WriteFile(filepath.Join(bindir, "tau"), []byte(fakeTau), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_TEST_BAD_REFLECTION", "2") // tick self-heals without a model

	c := newTestCompany(t)
	c.tasks = assignFailBackend{&localBackend{c: c}}
	writeAgentFile(t, c, "cto", "---\nname: cto\ntitle: CTO\nimplements: true\n---\n")

	if _, err := c.tasks.AddTask("Fix the flaky test", ""); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	var worked bool
	var err error
	out := captureStderr(t, func() {
		worked, err = reconcileOnce(c)
	})
	if err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}
	if !strings.Contains(out, "assign boom") {
		t.Errorf("stderr should log the assign failure, got:\n%s", out)
	}
	// The unrouted open task is still picked up by the tick loop's fallback.
	if !worked {
		t.Error("tick loop should still claim and work the unrouted open task")
	}
}

// TestReconcileOnce_TickErrorLogged verifies that a per-agent tick failure (here:
// tau missing from PATH) is logged and skipped rather than aborting the pass.
func TestReconcileOnce_TickErrorLogged(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_PROVIDER", "")
	t.Setenv("MAGO_MODEL", "")
	t.Setenv("MAGO_TEST_BAD_REFLECTION", "")
	t.Setenv("PATH", t.TempDir()) // no tau binary

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "cto", "---\nname: cto\ntitle: CTO\nimplements: true\n---\n")

	task, err := c.tasks.AddTask("Finish the migration", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := c.tasks.Claim(task, "cto"); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	var worked bool
	err = nil
	out := captureStderr(t, func() {
		worked, err = reconcileOnce(c)
	})
	if err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}
	if worked {
		t.Error("a failing tick should report worked=false")
	}
	if !strings.Contains(out, "[tick] cto:") {
		t.Errorf("stderr should log the tick error, got:\n%s", out)
	}
}
