package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdTask_AddTaskSaveError verifies cmdTask surfaces the backend's AddTask
// error instead of reporting success. tasks/ is replaced by a regular file so
// the save fails with ENOTDIR — deterministic even when tests run as root.
func TestCmdTask_AddTaskSaveError(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_TASK_LABEL", "")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "tasks"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := cmdTask([]string{"-C", dir, "add", "doomed task"})
	if err == nil {
		t.Fatal("expected error when the task file cannot be written")
	}
	if !strings.Contains(err.Error(), "task-1.md") {
		t.Errorf("error should name the task file that failed, got: %v", err)
	}
}

// TestCmdTask_ProjectFlagMidTitle verifies --project can appear between title
// words: it is extracted (not joined into the title) and the remaining words
// still form the title.
func TestCmdTask_ProjectFlagMidTitle(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_TASK_LABEL", "")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	out := captureStdout(t, func() {
		if err := cmdTask([]string{"-C", dir, "add", "fix", "--project", "web", "the", "thing"}); err != nil {
			t.Fatalf("cmdTask: %v", err)
		}
	})
	if !strings.Contains(out, "created task #1: fix the thing [project: web]") {
		t.Errorf("unexpected output: %q", out)
	}

	b, err := os.ReadFile(filepath.Join(dir, "tasks", "task-1.md"))
	if err != nil {
		t.Fatalf("task file missing: %v", err)
	}
	if !strings.Contains(string(b), "project: web") {
		t.Errorf("task frontmatter missing project, got:\n%s", b)
	}
	if strings.Contains(string(b), "--project") {
		t.Errorf("flag leaked into the task, got:\n%s", b)
	}
}

// TestCmdTask_ProjectFlagTrailingNoValue documents the edge where --project is
// the last token: with no value to consume it is treated as a title word, so
// the task is created without a project rather than erroring.
func TestCmdTask_ProjectFlagTrailingNoValue(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_TASK_LABEL", "")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	out := captureStdout(t, func() {
		if err := cmdTask([]string{"-C", dir, "add", "fix", "thing", "--project"}); err != nil {
			t.Fatalf("cmdTask: %v", err)
		}
	})
	if !strings.Contains(out, "created task #1: fix thing --project") {
		t.Errorf("unexpected output: %q", out)
	}
	if strings.Contains(out, "[project:") {
		t.Errorf("expected no project tag, got: %q", out)
	}
}

// TestCmdTask_ProjectFlagWhitespaceValue verifies a whitespace-only --project
// value is trimmed to empty, so the task is created without a project tag.
func TestCmdTask_ProjectFlagWhitespaceValue(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_TASK_LABEL", "")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	out := captureStdout(t, func() {
		if err := cmdTask([]string{"-C", dir, "add", "fix", "thing", "--project", "   "}); err != nil {
			t.Fatalf("cmdTask: %v", err)
		}
	})
	if !strings.Contains(out, "created task #1: fix thing") {
		t.Errorf("unexpected output: %q", out)
	}
	if strings.Contains(out, "[project:") {
		t.Errorf("whitespace project should be dropped, got: %q", out)
	}
}
