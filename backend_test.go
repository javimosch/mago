package main

import (
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

func taskIDs(tasks []*Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.ID
	}
	return out
}
