package main

import (
	"os"
	"path/filepath"
	"testing"
)

const testMissionState = "# co\n\n## Mission\nShip a delightful CLI.\n\n## Shipped\n(none)\n\n## In flight\n(none)\n\n## Decisions\n(none)\n\n## Activity log\n"

// fakeTau writes a fake `tau` binary into a temp dir and prepends it to PATH so
// tauComplete resolves it instead of a real provider. The script body controls
// what the "model" returns (last NDJSON line with a "content" field wins).
func fakeTau(t *testing.T, scriptBody string) {
	t.Helper()
	bindir := t.TempDir()
	script := filepath.Join(bindir, "tau")
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("write fake tau: %v", err)
	}
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))
}

// TestProposeBacklog_IssueCapOverride verifies an operator-set MAGO_ISSUE_CAP
// replaces the default backlog cap: one active task with cap 1 means nothing
// new is proposed.
func TestProposeBacklog_IssueCapOverride(t *testing.T) {
	t.Setenv("MAGO_ISSUE_CAP", "1")

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	if err := os.WriteFile(c.stateFile(), []byte(testMissionState), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if _, err := c.tasks.AddTask("already active", ""); err != nil {
		t.Fatalf("add task: %v", err)
	}
	writeAgentFile(t, c, "hop", "---\nname: hop\ntitle: Head of Product\nplans: true\n---\nYou plan.")

	if got := c.proposeBacklog(); got != 0 {
		t.Errorf("proposeBacklog() with issue cap reached = %d, want 0", got)
	}
}

// TestProposeBacklog_PlannerError verifies a failing planner (tau exits non-zero)
// is logged and swallowed — proposeBacklog returns 0 rather than propagating.
func TestProposeBacklog_PlannerError(t *testing.T) {
	fakeTau(t, "#!/bin/sh\nexit 1\n")

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	if err := os.WriteFile(c.stateFile(), []byte(testMissionState), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	writeAgentFile(t, c, "hop", "---\nname: hop\ntitle: Head of Product\nplans: true\n---\nYou plan.")

	if got := c.proposeBacklog(); got != 0 {
		t.Errorf("proposeBacklog() with failing planner = %d, want 0", got)
	}
}

// TestProposeBacklog_SkipsDuplicate verifies a planner proposal that restates an
// already-open title is skipped instead of filed twice.
func TestProposeBacklog_SkipsDuplicate(t *testing.T) {
	fakeTau(t, "#!/bin/sh\nprintf '%s\\n' '{\"content\":\"Fix login bug\\nBrand new task\"}'\n")

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	if err := os.WriteFile(c.stateFile(), []byte(testMissionState), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	writeAgentFile(t, c, "hop", "---\nname: hop\ntitle: Head of Product\nplans: true\n---\nYou plan.")
	if _, err := c.tasks.AddTask("Fix login bug", ""); err != nil {
		t.Fatalf("add task: %v", err)
	}

	if got := c.proposeBacklog(); got != 1 {
		t.Errorf("proposeBacklog() = %d, want 1 (duplicate title skipped)", got)
	}
	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(ts) != 2 {
		t.Fatalf("expected 2 tasks total (1 existing + 1 filed), got %d", len(ts))
	}
}

// TestProposeBacklog_PerCycleCap verifies the planner may emit more titles than the
// per-cycle cap but only `want` are filed (MAGO_PROACTIVE_MAX=1).
func TestProposeBacklog_PerCycleCap(t *testing.T) {
	t.Setenv("MAGO_PROACTIVE_MAX", "1")
	fakeTau(t, "#!/bin/sh\nprintf '%s\\n' '{\"content\":\"Task one\\nTask two\\nTask three\"}'\n")

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	if err := os.WriteFile(c.stateFile(), []byte(testMissionState), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	writeAgentFile(t, c, "hop", "---\nname: hop\ntitle: Head of Product\nplans: true\n---\nYou plan.")

	if got := c.proposeBacklog(); got != 1 {
		t.Errorf("proposeBacklog() = %d, want 1 with MAGO_PROACTIVE_MAX=1", got)
	}
}

// TestPlannerAgent_NoAgentsDir verifies plannerAgent returns nil when the company
// has no .mago/agents directory at all (loadAgentNames error path).
func TestPlannerAgent_NoAgentsDir(t *testing.T) {
	c := &Company{Dir: t.TempDir()}
	if got := c.plannerAgent(); got != nil {
		t.Errorf("plannerAgent() = %v, want nil without an agents dir", got)
	}
}

// TestSetMission_MissingState verifies setMission is a no-op when STATE.md does
// not exist — it must not create the file as a side effect.
func TestSetMission_MissingState(t *testing.T) {
	c := &Company{Dir: t.TempDir()}
	c.setMission("New mission")
	if _, err := os.Stat(c.stateFile()); !os.IsNotExist(err) {
		t.Errorf("setMission created %s unexpectedly (stat err=%v)", c.stateFile(), err)
	}
}
