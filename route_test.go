package main

import (
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
