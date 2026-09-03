package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helpers

func makeAgent(name, title string, reviews, plans, implements bool) *Agent {
	return &Agent{
		Name:       name,
		Title:      title,
		Provider:   "deepseek",
		Model:      "deepseek-chat",
		Reviews:    reviews,
		Plans:      plans,
		Implements: implements,
	}
}

func makeTask(id, title string) *Task {
	return &Task{ID: id, Title: title, Status: "open"}
}

// --- isReviewerRole ---

func TestIsReviewerRole_FlagTrue(t *testing.T) {
	a := makeAgent("alice", "Engineer", true, false, false)
	if !isReviewerRole(a) {
		t.Error("reviews:true agent must be treated as reviewer")
	}
}

func TestIsReviewerRole_NameContainsReview(t *testing.T) {
	a := makeAgent("head-of-org-engineering-reviewer", "Engineer", false, false, false)
	if !isReviewerRole(a) {
		t.Error("name containing 'review' must be treated as reviewer")
	}
}

func TestIsReviewerRole_TitleContainsReview(t *testing.T) {
	a := makeAgent("bob", "Code Reviewer", false, false, false)
	if !isReviewerRole(a) {
		t.Error("title containing 'review' must be treated as reviewer")
	}
}

func TestIsReviewerRole_ImplementerIsNotReviewer(t *testing.T) {
	a := makeAgent("cto", "Chief Technology Officer", false, false, true)
	if isReviewerRole(a) {
		t.Error("implementer must not be treated as reviewer")
	}
}

func TestIsReviewerRole_PlannerIsNotReviewer(t *testing.T) {
	a := makeAgent("hop", "Head of Product", false, true, false)
	if isReviewerRole(a) {
		t.Error("planner must not be treated as reviewer")
	}
}

func TestIsReviewerRole_PlainAgentIsNotReviewer(t *testing.T) {
	a := makeAgent("cmo", "Chief Marketing Officer", false, false, false)
	if isReviewerRole(a) {
		t.Error("plain non-reviewer agent must not be treated as reviewer")
	}
}

// --- plannerName ---

func TestPlannerName_EmptyRoster(t *testing.T) {
	if got := plannerName(nil); got != "" {
		t.Errorf("empty roster: got %q, want %q", got, "")
	}
}

func TestPlannerName_NoPlanner(t *testing.T) {
	agents := []*Agent{
		makeAgent("cto", "CTO", false, false, true),
		makeAgent("cmo", "CMO", false, false, false),
	}
	if got := plannerName(agents); got != "" {
		t.Errorf("no planner in roster: got %q, want %q", got, "")
	}
}

func TestPlannerName_SinglePlanner(t *testing.T) {
	agents := []*Agent{
		makeAgent("cto", "CTO", false, false, true),
		makeAgent("hop", "Head of Product", false, true, false),
	}
	if got := plannerName(agents); got != "hop" {
		t.Errorf("got %q, want %q", got, "hop")
	}
}

func TestPlannerName_FirstPlannerWins(t *testing.T) {
	agents := []*Agent{
		makeAgent("hop", "Head of Product", false, true, false),
		makeAgent("pod", "Product Owner", false, true, false),
	}
	if got := plannerName(agents); got != "hop" {
		t.Errorf("first planner in roster should win: got %q, want %q", got, "hop")
	}
}

// --- implementerName ---

func TestImplementerName_EmptyRoster(t *testing.T) {
	if got := implementerName(nil); got != "" {
		t.Errorf("empty roster: got %q, want %q", got, "")
	}
}

func TestImplementerName_NoImplementer(t *testing.T) {
	agents := []*Agent{
		makeAgent("hop", "Head of Product", false, true, false),
		makeAgent("cmo", "CMO", false, false, false),
	}
	if got := implementerName(agents); got != "" {
		t.Errorf("no implementer: got %q, want %q", got, "")
	}
}

func TestImplementerName_SingleImplementer(t *testing.T) {
	agents := []*Agent{
		makeAgent("cto", "CTO", false, false, true),
		makeAgent("cmo", "CMO", false, false, false),
	}
	if got := implementerName(agents); got != "cto" {
		t.Errorf("got %q, want %q", got, "cto")
	}
}

func TestImplementerName_FirstImplementerWins(t *testing.T) {
	agents := []*Agent{
		makeAgent("staff", "Staff Eng", false, false, true),
		makeAgent("cto", "CTO", false, false, true),
	}
	if got := implementerName(agents); got != "staff" {
		t.Errorf("first implementer in roster should win: got %q, want %q", got, "staff")
	}
}

// --- routeTask (LLM-free paths) ---
//
// tauComplete fails in the test environment (no API key), so routeTask always
// exercises its deterministic fallback: implementerName(candidates) → candidates[0].
// These tests verify reviewer exclusion and the fallback ordering without mocking.

func TestRouteTask_ExcludesReviewers(t *testing.T) {
	reviewer := makeAgent("hoer", "Head of Org Engineering", true, false, false)
	impl := makeAgent("cto", "CTO", false, false, true)
	task := makeTask("1", "Add route_test.go")

	got := routeTask(impl, task, []*Agent{reviewer, impl})
	if got != "cto" {
		t.Errorf("reviewer should be excluded; got %q, want %q", got, "cto")
	}
}

func TestRouteTask_AllReviewersFallbackToFullRoster(t *testing.T) {
	// When every agent is a reviewer the router must still route (uses full roster).
	r1 := makeAgent("rev1", "Reviewer Alpha", true, false, false)
	r2 := makeAgent("rev2", "Reviewer Beta", true, false, false)
	task := makeTask("2", "Polish the README")

	got := routeTask(r1, task, []*Agent{r1, r2})
	if got == "" {
		t.Error("all-reviewer roster: should still return a non-empty owner")
	}
}

func TestRouteTask_ImplementerFallbackWithNoLLM(t *testing.T) {
	// Mixed roster without a named reviewer: implementer should win via fallback.
	cmo := makeAgent("cmo", "Chief Marketing Officer", false, false, false)
	cto := makeAgent("cto", "Chief Technology Officer", false, false, true)
	task := makeTask("3", "Fix the authentication bug")

	got := routeTask(cto, task, []*Agent{cmo, cto})
	if got != "cto" {
		t.Errorf("implementer fallback: got %q, want %q", got, "cto")
	}
}

func TestRouteTask_NoImplementerFallsToFirstCandidate(t *testing.T) {
	// No implementer flag: should fall back to first non-reviewer candidate.
	cmo := makeAgent("cmo", "Chief Marketing Officer", false, false, false)
	hop := makeAgent("hop", "Head of Product", false, true, false)
	task := makeTask("4", "Write announcement copy")

	// carrier is cmo (any non-reviewer); LLM will fail, so first candidate wins.
	got := routeTask(cmo, task, []*Agent{cmo, hop})
	if got == "" {
		t.Error("should fall back to first candidate when no implementer")
	}
}

// --- reconcileOnce ---

func TestReconcileOnce_NoAgents(t *testing.T) {
	c := newTestCompany(t)
	_, err := reconcileOnce(c)
	if err == nil {
		t.Fatal("reconcileOnce with no agents should return an error")
	}
	if !strings.Contains(err.Error(), "no agents") {
		t.Errorf("error should mention 'no agents', got: %v", err)
	}
}

func TestCmdTick_NoAgents(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)
	err := cmdTick([]string{"-C", c.Dir})
	if err == nil {
		t.Fatal("cmdTick with no agents should return an error")
	}
	if !strings.Contains(err.Error(), "no agents") {
		t.Errorf("error should mention 'no agents', got: %v", err)
	}
}

// TestReconcileOnce_AgentWithNoTasks verifies that reconcileOnce runs through an
// agent roster, finds nothing to do, and returns cleanly without touching a model.
func TestReconcileOnce_AgentWithNoTasks(t *testing.T) {
	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "cto", "---\ntitle: CTO\nimplements: true\n---\n")

	worked, err := reconcileOnce(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if worked {
		t.Error("reconcileOnce with no tasks should return worked=false")
	}
}

// TestReconcileOnce_ReviewerBouncesTask verifies that an open task routed to a
// review-only agent is immediately bounced and the tick reports work, without
// ever calling runTau for that agent.
func TestReconcileOnce_ReviewerBouncesTask(t *testing.T) {
	bindir := t.TempDir()
	script := filepath.Join(bindir, "tau")
	// tauComplete returns the last JSON object with a "content" field.
	// A single line with the reviewer name makes routeTask pick it.
	body := "#!/bin/sh\nprintf '%s\\n' '{\"content\":\"rev\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake tau: %v", err)
	}
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "rev", "---\nname: rev\ntitle: Head of Org Engineering\nreviews: true\n---\n")

	if _, err := c.tasks.AddTask("Triage the review backlog", ""); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	worked, err := reconcileOnce(c)
	if err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}
	if !worked {
		t.Error("reconcileOnce should report worked=true when a task is bounced")
	}

	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(ts) != 1 {
		t.Fatalf("expected 1 task, got %d", len(ts))
	}
	if ts[0].Assignee != "" {
		t.Errorf("bounced task should be unassigned, got assignee %q", ts[0].Assignee)
	}
	if ts[0].Status != "open" {
		t.Errorf("bounced task should be open, got status %q", ts[0].Status)
	}
}

// TestReconcileOnce_UnknownAssigneeReroutes verifies that a task assigned to a
// non-existent agent is bounced, re-routed to a real implementer, and then claimed.
func TestReconcileOnce_UnknownAssigneeReroutes(t *testing.T) {
	bindir := t.TempDir()
	script := filepath.Join(bindir, "tau")
	// tauComplete returns the last JSON object with a "content" field.
	body := "#!/bin/sh\nprintf '%s\\n' '{\"content\":\"cto\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake tau: %v", err)
	}
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))
	t.Setenv("MAGO_TEST_BAD_REFLECTION", "2")
	t.Setenv("MAGO_GH_REPO", "")

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "cto", "---\nname: cto\ntitle: CTO\nimplements: true\n---\n")

	task, err := c.tasks.AddTask("Fix the auth flow", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := c.tasks.Assign(task, "ghost"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	worked, err := reconcileOnce(c)
	if err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}
	if !worked {
		t.Error("reconcileOnce should report worked=true after re-routing to a real agent")
	}

	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(ts) != 1 {
		t.Fatalf("expected 1 task, got %d", len(ts))
	}
	if ts[0].Assignee != "cto" {
		t.Errorf("task should be assigned to cto, got %q", ts[0].Assignee)
	}
	if ts[0].Status != "in_progress" {
		t.Errorf("task should be claimed/in_progress, got status %q", ts[0].Status)
	}
}

// TestReconcileOnce_InProgressTaskResumes verifies that an in-progress task that
// is already assigned to a known agent is not re-routed; the agent resumes work.
func TestReconcileOnce_InProgressTaskResumes(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_TEST_BAD_REFLECTION", "2")

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "cto", "---\nname: cto\ntitle: CTO\nimplements: true\n---\n")

	task, err := c.tasks.AddTask("Finish the dashboard", "")
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
		t.Error("reconcileOnce should report worked=true when an in-progress task resumes")
	}

	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(ts) != 1 {
		t.Fatalf("expected 1 task, got %d", len(ts))
	}
	if ts[0].Assignee != "cto" {
		t.Errorf("task should stay assigned to cto, got %q", ts[0].Assignee)
	}
	if ts[0].Status != "in_progress" {
		t.Errorf("task should stay in_progress, got status %q", ts[0].Status)
	}
}

// TestReconcileOnce_DoneTaskSkipped verifies that a completed task is ignored by
// the router and leaves the agent idle, so no model call is attempted.
func TestReconcileOnce_DoneTaskSkipped(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "cto", "---\nname: cto\ntitle: CTO\nimplements: true\n---\n")

	task, err := c.tasks.AddTask("Polish the README", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := c.tasks.SetStatus(task, "done"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	worked, err := reconcileOnce(c)
	if err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}
	if worked {
		t.Error("reconcileOnce with only a done task should report worked=false")
	}

	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(ts) != 1 {
		t.Fatalf("expected 1 task, got %d", len(ts))
	}
	if ts[0].Status != "done" {
		t.Errorf("done task should stay done, got status %q", ts[0].Status)
	}
}
