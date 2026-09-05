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

// stubListTasks injects tasks carrying GitHub-label-derived state (mago:go /
// mago:clarify) that the local file backend can't express, and records ClearClarify
// calls. Other TaskBackend methods fall through to the embedded localBackend.
type stubListTasks struct {
	*localBackend
	list    []*Task
	cleared int
}

func (s *stubListTasks) ListTasks() ([]*Task, error) { return s.list, nil }

func (s *stubListTasks) ClearClarify(t *Task) error {
	s.cleared++
	t.Clarify = false
	t.Go = false
	return nil
}

// TestReconcileOnce_GoPromotesClarifyTask verifies that a task still in clarification
// (needs_human + mago:clarify) that receives mago:go is promoted — its planning state
// dropped — rather than routed back to the planner.
func TestReconcileOnce_GoPromotesClarifyTask(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")

	c := newTestCompany(t)
	st := &stubListTasks{localBackend: &localBackend{c: c}}
	c.tasks = st
	writeAgentFile(t, c, "cto", "---\nname: cto\ntitle: CTO\nimplements: true\n---\n")

	st.list = []*Task{{ID: "9", Title: "Spec the API", Status: "needs_human", Clarify: true, Go: true}}

	if _, err := reconcileOnce(c); err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}
	if st.cleared != 1 {
		t.Errorf("ClearClarify calls = %d, want 1 for a mago:go task still in clarification", st.cleared)
	}
}

// TestReconcileOnce_AllAgentsUnloadable verifies that an agents dir whose .md files all
// fail to load (e.g. invalid frontmatter) produces a clean error rather than a panic on
// agents[0] during routing.
func TestReconcileOnce_AllAgentsUnloadable(t *testing.T) {
	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	// "provder" is an unknown frontmatter key, so loadAgent rejects the file even though
	// loadAgentNames listed it.
	writeAgentFile(t, c, "broken", "---\nname: broken\nprovder: openai\n---\n")

	worked, err := reconcileOnce(c)
	if err == nil {
		t.Fatal("reconcileOnce should error when no agent files load, got nil")
	}
	if worked {
		t.Error("reconcileOnce should report worked=false when no agents load")
	}
	if !strings.Contains(err.Error(), c.agentsDir()) {
		t.Errorf("error should name the agents dir, got: %v", err)
	}
}
