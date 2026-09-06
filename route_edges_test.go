package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeOnPath drops an executable shell script named `name` into a temp dir and
// prepends it to PATH for the test, so code that shells out (tauComplete -> tau,
// repoOpenMagoPRs -> gh) gets a deterministic, instant stand-in instead of a real
// model call or network request.
func fakeOnPath(t *testing.T, name, body string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// stubBackend drives reconcileOnce's routing/tick edges without touching disk or
// GitHub: ListTasks serves a fixed list, Assign/Bounce record calls and can be
// forced to fail, and PickActiveTask can be forced to fail so runTick's error
// propagates back into the reconcile loop.
type stubBackend struct {
	*localBackend
	list        []*Task
	assignCalls int
	assignedTo  []string
	assignErr   error
	bounced     int
	bounceErr   error
	pickErr     error
}

func (s *stubBackend) ListTasks() ([]*Task, error) { return s.list, nil }

func (s *stubBackend) Assign(t *Task, agent string) error {
	s.assignCalls++
	if s.assignErr != nil {
		return s.assignErr
	}
	s.assignedTo = append(s.assignedTo, agent)
	return nil
}

func (s *stubBackend) Bounce(t *Task) error {
	s.bounced++
	if s.bounceErr != nil {
		return s.bounceErr
	}
	t.Assignee = ""
	t.Status = "open"
	return nil
}

func (s *stubBackend) PickActiveTask(agent string) (*Task, error) {
	if s.pickErr != nil {
		return nil, s.pickErr
	}
	return s.localBackend.PickActiveTask(agent)
}

// TestCmdTick_DanglingDirFlag verifies cmdTick surfaces the parseCompanyDir
// error for a bare -C with no directory value.
func TestCmdTick_DanglingDirFlag(t *testing.T) {
	err := cmdTick([]string{"-C"})
	if err == nil {
		t.Fatal("cmdTick with a dangling -C should error")
	}
	if !strings.Contains(err.Error(), "-C") {
		t.Errorf("error should mention -C, got: %v", err)
	}
}

// TestCmdTick_NotACompany verifies cmdTick surfaces loadCompany's error when the
// target directory was never `mago init`'d.
func TestCmdTick_NotACompany(t *testing.T) {
	err := cmdTick([]string{"-C", t.TempDir()})
	if err == nil {
		t.Fatal("cmdTick on a directory without .mago/ should error")
	}
	if !strings.Contains(err.Error(), ".mago") {
		t.Errorf("error should mention .mago/, got: %v", err)
	}
}

// TestReconcileOnce_BounceErrorContinues verifies that a task assigned to an
// agent outside the roster is bounced, and that a Bounce failure is logged and
// skipped rather than aborting the whole reconcile.
func TestReconcileOnce_BounceErrorContinues(t *testing.T) {
	c := newTestCompany(t)
	sb := &stubBackend{localBackend: &localBackend{c: c}, bounceErr: errors.New("bounce boom")}
	c.tasks = sb
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Dev\nimplements: true\n---\n")

	sb.list = []*Task{{ID: "7", Title: "Half-done fix", Status: "in_progress", Assignee: "ghost"}}

	worked, err := reconcileOnce(c)
	if err != nil {
		t.Fatalf("reconcileOnce should swallow a Bounce failure, got: %v", err)
	}
	if worked {
		t.Error("expected worked=false — no agent had a claimable task")
	}
	if sb.bounced != 1 {
		t.Errorf("Bounce calls = %d, want 1 for the ghost-assignee task", sb.bounced)
	}
	if sb.assignCalls != 0 {
		t.Errorf("Assign calls = %d, want 0 — a failed bounce must not fall through to routing", sb.assignCalls)
	}
}

// TestReconcileOnce_ClarifyTaskRoutesToPlanner verifies that an open task still
// in clarification (mago:clarify, no mago:go) is assigned to the planner agent
// directly — never to the implement router.
func TestReconcileOnce_ClarifyTaskRoutesToPlanner(t *testing.T) {
	c := newTestCompany(t)
	sb := &stubBackend{localBackend: &localBackend{c: c}}
	c.tasks = sb
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Dev\nimplements: true\n---\n")
	writeAgentFile(t, c, "planner", "---\nname: planner\ntitle: Head of Product\nplans: true\n---\n")

	sb.list = []*Task{{ID: "5", Title: "Spec the API", Status: "open", Clarify: true}}

	if _, err := reconcileOnce(c); err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}
	if len(sb.assignedTo) != 1 || sb.assignedTo[0] != "planner" {
		t.Errorf("assignedTo = %v, want [planner] — clarify tasks go to the planner", sb.assignedTo)
	}
}

// TestReconcileOnce_AssignErrorContinues verifies that an Assign failure during
// routing is logged and skipped, not propagated — one unroutable task must not
// stop the rest of the reconcile.
func TestReconcileOnce_AssignErrorContinues(t *testing.T) {
	fakeOnPath(t, "tau", `printf '{"content":"dev"}\n'`)

	c := newTestCompany(t)
	sb := &stubBackend{localBackend: &localBackend{c: c}, assignErr: errors.New("assign boom")}
	c.tasks = sb
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Dev\nimplements: true\n---\n")

	sb.list = []*Task{{ID: "3", Title: "Fix the bug", Status: "open"}}

	worked, err := reconcileOnce(c)
	if err != nil {
		t.Fatalf("reconcileOnce should swallow an Assign failure, got: %v", err)
	}
	if worked {
		t.Error("expected worked=false — the agent had nothing claimed to tick on")
	}
	if sb.assignCalls != 1 {
		t.Errorf("Assign calls = %d, want 1", sb.assignCalls)
	}
}

// TestReconcileOnce_PRCapHoldsTask verifies the per-repo PR backpressure: when a
// repo already has its cap of open mago PRs, open tasks are held unassigned so
// the worker doesn't pile past the cap.
func TestReconcileOnce_PRCapHoldsTask(t *testing.T) {
	t.Setenv("MAGO_PR_CAP", "1")
	fakeOnPath(t, "gh", `printf '[{"headRefName":"mago/task-9"}]\n'`)

	c := newTestCompany(t)
	c.ghRepo = "o/r"
	sb := &stubBackend{localBackend: &localBackend{c: c}}
	c.tasks = sb
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Dev\nimplements: true\n---\n")

	sb.list = []*Task{{ID: "4", Title: "Ship the fix", Status: "open"}}

	if _, err := reconcileOnce(c); err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}
	if sb.assignCalls != 0 {
		t.Errorf("Assign calls = %d, want 0 — the repo is at its PR cap so the task must be held", sb.assignCalls)
	}
}

// TestReconcileOnce_TickErrorContinues verifies that an agent whose tick fails
// is logged and skipped — one bad tick must not abort the remaining agents or
// fail the reconcile.
func TestReconcileOnce_TickErrorContinues(t *testing.T) {
	c := newTestCompany(t)
	sb := &stubBackend{localBackend: &localBackend{c: c}, pickErr: errors.New("pick boom")}
	c.tasks = sb
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Dev\nimplements: true\n---\n")

	worked, err := reconcileOnce(c)
	if err != nil {
		t.Fatalf("reconcileOnce should swallow a per-agent tick failure, got: %v", err)
	}
	if worked {
		t.Error("expected worked=false when every agent's tick failed")
	}
}

// TestRouteTask_EmptyRoster verifies routeTask returns "" (rather than panicking
// or inventing an owner) when the candidate roster is empty.
func TestRouteTask_EmptyRoster(t *testing.T) {
	fakeOnPath(t, "tau", "exit 1")

	if got := routeTask(&Agent{Name: "x"}, &Task{Title: "anything"}, nil); got != "" {
		t.Errorf("routeTask with an empty roster = %q, want \"\"", got)
	}
}
