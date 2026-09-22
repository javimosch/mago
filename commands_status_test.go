package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdStatus_WithStateFile verifies that an existing STATE.md is printed
// verbatim instead of the "(no STATE.md)" placeholder.
func TestCmdStatus_WithStateFile(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)

	state := "# co-mago — company state\n\n## Mission\nShip it.\n"
	if err := os.WriteFile(c.stateFile(), []byte(state), 0o644); err != nil {
		t.Fatalf("write STATE.md: %v", err)
	}

	out := captureStdout(t, func() {
		if err := cmdStatus([]string{"-C", c.Dir}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})

	if !strings.Contains(out, "## Mission") || !strings.Contains(out, "Ship it.") {
		t.Errorf("expected STATE.md contents in output, got: %q", out)
	}
	if strings.Contains(out, "(no STATE.md)") {
		t.Errorf("unexpected missing-state marker, got: %q", out)
	}
}

// TestCmdStatus_BlankStateFile verifies that a STATE.md containing only
// whitespace falls back to the "(no STATE.md)" placeholder.
func TestCmdStatus_BlankStateFile(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)

	if err := os.WriteFile(c.stateFile(), []byte("  \n\t\n"), 0o644); err != nil {
		t.Fatalf("write STATE.md: %v", err)
	}

	out := captureStdout(t, func() {
		if err := cmdStatus([]string{"-C", c.Dir}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})

	if !strings.Contains(out, "(no STATE.md)") {
		t.Errorf("expected missing-state marker for blank STATE.md, got: %q", out)
	}
}

// TestCmdStatus_ProjectNoRepo verifies that a project configured without a repo
// (e.g. a hand-edited projects.json) prints the "(no repo)" placeholder rather
// than a blank target.
func TestCmdStatus_ProjectNoRepo(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)

	if err := os.WriteFile(c.projectsConfigFile(), []byte(`{"legacy":{"repo":"","mirror_issue":true}}`), 0o644); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}

	out := captureStdout(t, func() {
		if err := cmdStatus([]string{"-C", c.Dir}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})

	if !strings.Contains(out, "legacy -> (no repo)") {
		t.Errorf("expected 'legacy -> (no repo)', got: %q", out)
	}
}

// TestCmdStatus_TaskWithAssignee verifies that a claimed task prints its
// assignee instead of the "-" placeholder.
func TestCmdStatus_TaskWithAssignee(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)

	task := "---\nid: 1\ntitle: claimed work\nstatus: in_progress\nassignee: cto\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(c.tasksDir(), "task-1.md"), []byte(task), 0o644); err != nil {
		t.Fatalf("write task: %v", err)
	}

	out := captureStdout(t, func() {
		if err := cmdStatus([]string{"-C", c.Dir}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})

	if !strings.Contains(out, "#1 [in_progress] claimed work (assignee: cto)") {
		t.Errorf("expected task with assignee, got: %q", out)
	}
}

// TestCmdStatus_ListTasksError verifies that cmdStatus surfaces a ListTasks
// failure (here: tasks/ replaced by a regular file) instead of printing a
// half-rendered status.
func TestCmdStatus_ListTasksError(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)

	if err := os.Remove(c.tasksDir()); err != nil {
		t.Fatalf("remove tasks dir: %v", err)
	}
	if err := os.WriteFile(c.tasksDir(), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write tasks file: %v", err)
	}

	err := cmdStatus([]string{"-C", c.Dir})
	if err == nil {
		t.Fatal("expected error when tasks/ is not a directory")
	}
	if !strings.Contains(err.Error(), "tasks") {
		t.Errorf("error = %q, want it to name the tasks path", err.Error())
	}
}
