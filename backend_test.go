package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestLocalBackend(t *testing.T) (*localBackend, *Company) {
	t.Helper()
	dir := t.TempDir()
	c := &Company{Dir: dir}
	if err := os.MkdirAll(c.tasksDir(), 0o755); err != nil {
		t.Fatalf("create tasks dir: %v", err)
	}
	if err := os.MkdirAll(c.inboxDir(), 0o755); err != nil {
		t.Fatalf("create inbox dir: %v", err)
	}
	return &localBackend{c: c}, c
}

func TestLocalBackendAddAndList(t *testing.T) {
	b, _ := newTestLocalBackend(t)

	t1, err := b.AddTask("first task", "p1")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if t1.ID != "1" || t1.Title != "first task" || t1.Project != "p1" || t1.Status != "open" {
		t.Fatalf("unexpected first task: %+v", t1)
	}

	t2, err := b.AddTask("second task", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if t2.ID != "2" || t2.Project != "" {
		t.Fatalf("unexpected second task: %+v", t2)
	}

	tasks, err := b.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("want 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ID != "1" || tasks[1].ID != "2" {
		t.Fatalf("tasks not sorted: %#v", tasks)
	}
}

func TestLocalBackendFindTask(t *testing.T) {
	b, _ := newTestLocalBackend(t)
	_, _ = b.AddTask("find me", "")

	task, err := b.FindTask("1")
	if err != nil {
		t.Fatalf("FindTask(1): %v", err)
	}
	if task.Title != "find me" {
		t.Fatalf("want title %q, got %q", "find me", task.Title)
	}

	if _, err := b.FindTask("99"); err == nil || !strings.Contains(err.Error(), "task #99 not found") {
		t.Fatalf("FindTask(99) should fail with not-found error, got: %v", err)
	}
}

func TestLocalBackendPickActiveTask(t *testing.T) {
	b, _ := newTestLocalBackend(t)

	// One open, unassigned task.
	t1, _ := b.AddTask("open unassigned", "")
	picked, err := b.PickActiveTask("cto")
	if err != nil {
		t.Fatalf("PickActiveTask: %v", err)
	}
	if picked == nil || picked.ID != t1.ID {
		t.Fatalf("want task %s, got %v", t1.ID, picked)
	}

	// Assigned task is preferred over unassigned open.
	t2, _ := b.AddTask("assigned open", "")
	if err := b.Assign(t2, "cmo"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	picked, _ = b.PickActiveTask("cmo")
	if picked == nil || picked.ID != t2.ID {
		t.Fatalf("want assigned open task %s, got %v", t2.ID, picked)
	}

	// In-progress task beats assigned open.
	if err := b.Claim(t1, "cto"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	picked, _ = b.PickActiveTask("cto")
	if picked == nil || picked.ID != t1.ID {
		t.Fatalf("want in-progress task %s, got %v", t1.ID, picked)
	}
}

func TestLocalBackendClaimAndProgress(t *testing.T) {
	b, _ := newTestLocalBackend(t)
	task, _ := b.AddTask("work", "")

	if err := b.Claim(task, "cto"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if task.Status != "in_progress" || task.Assignee != "cto" || task.ClaimedAt == "" {
		t.Fatalf("unexpected task after claim: %+v", task)
	}

	if err := b.RecordProgress(task, "cto", "started"); err != nil {
		t.Fatalf("RecordProgress: %v", err)
	}
	if !strings.Contains(task.Body, "started") {
		t.Fatalf("progress note missing from body: %s", task.Body)
	}

	if err := b.SetStatus(task, "blocked"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if task.Status != "blocked" {
		t.Fatalf("want status blocked, got %q", task.Status)
	}
}

func TestLocalBackendBounce(t *testing.T) {
	b, _ := newTestLocalBackend(t)
	task, _ := b.AddTask("reassign me", "")
	if err := b.Assign(task, "cmo"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := b.Bounce(task); err != nil {
		t.Fatalf("Bounce: %v", err)
	}
	if task.Assignee != "" || task.Status != "open" {
		t.Fatalf("unexpected task after bounce: %+v", task)
	}
}

func TestLocalBackendHITL(t *testing.T) {
	b, c := newTestLocalBackend(t)
	task, _ := b.AddTask("needs human", "")

	if err := b.RaiseHITL(task, "cto", "what colour?"); err != nil {
		t.Fatalf("RaiseHITL: %v", err)
	}
	if task.Status != "needs_human" {
		t.Fatalf("want status needs_human, got %q", task.Status)
	}
	if !strings.Contains(task.Body, "NEEDS HUMAN") {
		t.Fatalf("body missing NEEDS HUMAN marker: %s", task.Body)
	}

	pending, err := b.PendingHITL()
	if err != nil {
		t.Fatalf("PendingHITL: %v", err)
	}
	if len(pending) != 1 || !strings.Contains(pending[0], "what colour?") {
		t.Fatalf("unexpected pending HITL: %v", pending)
	}

	if _, err := os.Stat(filepath.Join(c.inboxDir(), "task-"+task.ID+".md")); err != nil {
		t.Fatalf("inbox file not created: %v", err)
	}

	if err := b.AnswerHITL(task.ID, "blue"); err != nil {
		t.Fatalf("AnswerHITL: %v", err)
	}

	pending, _ = b.PendingHITL()
	if len(pending) != 0 {
		t.Fatalf("want no pending HITL, got %d", len(pending))
	}

	task, _ = b.FindTask(task.ID)
	if task.Status != "in_progress" || !strings.Contains(task.Body, "HUMAN ANSWER") {
		t.Fatalf("task not resumed after answer: %+v, body: %s", task, task.Body)
	}
}

func TestLocalBackendClearClarify(t *testing.T) {
	b, _ := newTestLocalBackend(t)
	task, _ := b.AddTask("clarify", "")
	if err := b.ClearClarify(task); err != nil {
		t.Fatalf("ClearClarify should be a no-op, got %v", err)
	}
}
