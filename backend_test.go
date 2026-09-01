package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalBackendTaskPath(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	b := &localBackend{c: c}
	want := filepath.Join(dir, "tasks", "task-5.md")
	if got := b.taskPath("5"); got != want {
		t.Errorf("taskPath(5) = %q, want %q", got, want)
	}
}

func TestLocalBackendAddAndFind(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	if err := ensureDir(c.tasksDir()); err != nil {
		t.Fatalf("ensure tasks dir: %v", err)
	}
	b := &localBackend{c: c}

	t1, err := b.AddTask("first task", "p1")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if t1.ID != "1" || t1.Title != "first task" || t1.Project != "p1" || t1.Status != "open" {
		t.Errorf("first task metadata wrong: %+v", t1)
	}

	t2, err := b.AddTask("second task", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if t2.ID != "2" {
		t.Errorf("second task ID = %q, want 2", t2.ID)
	}

	got, err := b.FindTask("1")
	if err != nil {
		t.Fatalf("FindTask: %v", err)
	}
	if got.Title != "first task" {
		t.Errorf("FindTask title = %q, want %q", got.Title, "first task")
	}

	tasks, err := b.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 || tasks[0].ID != "1" || tasks[1].ID != "2" {
		t.Errorf("ListTasks = %v, want [1, 2]", taskIDs(tasks))
	}

	if _, err := b.FindTask("99"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("FindTask(99) error = %v, want %q", err, "not found")
	}
}

func TestLocalBackendLifecycle(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	if err := ensureDir(c.tasksDir()); err != nil {
		t.Fatalf("ensure tasks dir: %v", err)
	}
	b := &localBackend{c: c}

	task, err := b.AddTask("lifecycle task", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if err := b.Assign(task, "dev"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if task.Assignee != "dev" {
		t.Errorf("Assignee = %q, want dev", task.Assignee)
	}

	if picked, err := b.PickActiveTask("dev"); err != nil || picked == nil || picked.ID != task.ID {
		t.Fatalf("PickActiveTask(dev) = %v, %v", picked, err)
	}

	if err := b.Claim(task, "dev"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if task.Status != "in_progress" || task.Assignee != "dev" {
		t.Errorf("after Claim: status=%q assignee=%q", task.Status, task.Assignee)
	}

	if err := b.RecordProgress(task, "dev", "made progress"); err != nil {
		t.Fatalf("RecordProgress: %v", err)
	}
	if !strings.Contains(task.Body, "made progress") {
		t.Errorf("progress not recorded in body")
	}

	if err := b.Bounce(task); err != nil {
		t.Fatalf("Bounce: %v", err)
	}
	if task.Status != "open" || task.Assignee != "" {
		t.Errorf("after Bounce: status=%q assignee=%q", task.Status, task.Assignee)
	}

	if err := b.SetStatus(task, "blocked"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if task.Status != "blocked" {
		t.Errorf("status = %q, want blocked", task.Status)
	}

	if err := b.ClearClarify(task); err != nil {
		t.Fatalf("ClearClarify: %v", err)
	}
}

func TestLocalBackendHITL(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	if err := ensureDir(c.tasksDir()); err != nil {
		t.Fatalf("ensure tasks dir: %v", err)
	}
	b := &localBackend{c: c}

	task, err := b.AddTask("hitl task", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if err := b.RaiseHITL(task, "dev", "need input"); err != nil {
		t.Fatalf("RaiseHITL: %v", err)
	}
	if task.Status != "needs_human" {
		t.Errorf("status = %q, want needs_human", task.Status)
	}

	pending, err := b.PendingHITL()
	if err != nil {
		t.Fatalf("PendingHITL: %v", err)
	}
	if len(pending) != 1 || !strings.Contains(pending[0], "need input") {
		t.Errorf("PendingHITL = %v", pending)
	}

	if err := b.AnswerHITL(task.ID, "here is the answer"); err != nil {
		t.Fatalf("AnswerHITL: %v", err)
	}

	pending, err = b.PendingHITL()
	if err != nil {
		t.Fatalf("PendingHITL after answer: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("PendingHITL after answer = %v, want empty", pending)
	}

	task, err = b.FindTask(task.ID)
	if err != nil {
		t.Fatalf("FindTask after answer: %v", err)
	}
	if task.Status != "in_progress" || !strings.Contains(task.Body, "HUMAN ANSWER") {
		t.Errorf("after answer: status=%q body missing HUMAN ANSWER", task.Status)
	}
}

func TestLocalBackendListTasks_SkipsNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	if err := ensureDir(c.tasksDir()); err != nil {
		t.Fatalf("ensure tasks dir: %v", err)
	}
	b := &localBackend{c: c}

	if _, err := b.AddTask("markdown task", ""); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	// A non-markdown file and a subdirectory should be ignored by ListTasks.
	if err := os.WriteFile(filepath.Join(c.tasksDir(), "README.txt"), []byte("not a task"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(c.tasksDir(), "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	tasks, err := b.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("ListTasks = %v, want 1 task", taskIDs(tasks))
	}
	if tasks[0].Title != "markdown task" {
		t.Errorf("task title = %q, want %q", tasks[0].Title, "markdown task")
	}
}

func TestLocalBackendPickActiveTask(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	if err := ensureDir(c.tasksDir()); err != nil {
		t.Fatalf("ensure tasks dir: %v", err)
	}
	b := &localBackend{c: c}

	// No tasks -> nothing to pick.
	if picked, err := b.PickActiveTask("dev"); err != nil || picked != nil {
		t.Fatalf("PickActiveTask with no tasks = %v, %v; want nil", picked, err)
	}

	// Open tasks assigned to someone else are not picked.
	other, err := b.AddTask("other task", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := b.Assign(other, "other"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if picked, _ := b.PickActiveTask("dev"); picked != nil {
		t.Fatalf("PickActiveTask should not pick task assigned to other agent")
	}
	// Block it so the remaining assertions aren't biased by a task owned by "other".
	if err := b.SetStatus(other, "blocked"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	// Open task assigned to the agent is picked.
	mine, err := b.AddTask("my task", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := b.Assign(mine, "dev"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if picked, err := b.PickActiveTask("dev"); err != nil || picked == nil || picked.ID != mine.ID {
		t.Fatalf("PickActiveTask(dev) = %v, %v; want %s", picked, err, mine.ID)
	}

	// In-progress work with a matching assignee wins over open tasks.
	if err := b.Claim(mine, "dev"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if picked, err := b.PickActiveTask("dev"); err != nil || picked == nil || picked.ID != mine.ID {
		t.Fatalf("PickActiveTask(dev) after claim = %v, %v; want %s", picked, err, mine.ID)
	}

	// In-progress task with no assignee can be picked by any agent.
	unassigned, err := b.AddTask("unassigned in-progress", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	unassigned.Status = "in_progress"
	unassigned.Assignee = ""
	if err := b.save(unassigned); err != nil {
		t.Fatalf("save: %v", err)
	}
	// mine is still in-progress and assigned to dev, so it should still win.
	if picked, err := b.PickActiveTask("dev"); err != nil || picked == nil || picked.ID != mine.ID {
		t.Fatalf("PickActiveTask(dev) should prefer assigned in-progress = %v, %v", picked, err)
	}

	// Fallback to an unrouted open task when no in-progress work exists.
	if err := b.Bounce(mine); err != nil {
		t.Fatalf("Bounce: %v", err)
	}
	if picked, err := b.PickActiveTask("dev"); err != nil || picked == nil || picked.ID != unassigned.ID {
		t.Fatalf("PickActiveTask(dev) fallback = %v, %v; want %s", picked, err, unassigned.ID)
	}

	// A different agent cannot pick work assigned to dev; unassigned open tasks are
	// the fallback, so make sure none remain for this final assertion.
	if err := b.Bounce(unassigned); err != nil {
		t.Fatalf("Bounce: %v", err)
	}
	if err := b.Assign(unassigned, "dev"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := b.Assign(mine, "dev"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if picked, _ := b.PickActiveTask("other"); picked != nil {
		t.Fatalf("other agent should not pick tasks assigned to dev")
	}
}

func taskIDs(tasks []*Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.ID
	}
	return out
}
