package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProposeBacklog_ListTasksError verifies that a task-list failure is logged and
// causes proposeBacklog to return 0 without filing new issues.
func TestProposeBacklog_ListTasksError(t *testing.T) {
	c := newTestCompany(t)
	c.tasks = &stubListTasksErr{localBackend: &localBackend{c: c}, err: os.ErrNotExist}

	state := "# co\n\n## Mission\nShip a delightful CLI.\n\n## Shipped\n(none)\n\n## In flight\n(none)\n\n## Decisions\n(none)\n\n## Activity log\n"
	if err := os.WriteFile(c.stateFile(), []byte(state), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	var got int
	out := captureStderr(t, func() { got = c.proposeBacklog() })
	if got != 0 {
		t.Errorf("proposeBacklog() = %d, want 0", got)
	}
	if !strings.Contains(out, "list tasks:") {
		t.Errorf("stderr should report the list error, got:\n%s", out)
	}
}

// stubAddTaskErr wraps localBackend but forces AddTask to fail.
type stubAddTaskErr struct {
	*localBackend
}

func (s *stubAddTaskErr) AddTask(title, project string) (*Task, error) {
	return nil, fmt.Errorf("cannot create %q: %w", title, os.ErrPermission)
}

// TestProposeBacklog_AddTaskError verifies that a planner-proposed title is reported
// and skipped when AddTask fails, returning 0 new issues.
func TestProposeBacklog_AddTaskError(t *testing.T) {
	bindir := t.TempDir()
	script := filepath.Join(bindir, "tau")
	body := "#!/bin/sh\nprintf '%s\\n' '{\"content\":\"Implement the login flow\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake tau: %v", err)
	}
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))

	c := newTestCompany(t)
	c.tasks = &stubAddTaskErr{localBackend: &localBackend{c: c}}

	state := "# co\n\n## Mission\nShip a delightful CLI.\n\n## Shipped\n(none)\n\n## In flight\n(none)\n\n## Decisions\n(none)\n\n## Activity log\n"
	if err := os.WriteFile(c.stateFile(), []byte(state), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	writeAgentFile(t, c, "hop", "---\nname: hop\ntitle: Head of Product\nplans: true\nprovider: deepseek\n---\nYou plan.")

	var got int
	out := captureStderr(t, func() { got = c.proposeBacklog() })
	if got != 0 {
		t.Errorf("proposeBacklog() = %d, want 0", got)
	}
	if !strings.Contains(out, "permission denied") {
		t.Errorf("stderr should report the AddTask error, got:\n%s", out)
	}
}
