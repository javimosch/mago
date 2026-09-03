package main

import (
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
