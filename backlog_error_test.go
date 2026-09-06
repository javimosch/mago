package main

import (
	"os"
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
