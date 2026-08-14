package main

import (
	"os"
	"strings"
	"testing"
)

func TestLocalBackendTaskFileFormat(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	if err := ensureDir(c.tasksDir()); err != nil {
		t.Fatalf("ensure tasks dir: %v", err)
	}
	b := &localBackend{c: c}

	task, err := b.AddTask("ship widgets", "p1")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if task.ID != "1" {
		t.Errorf("ID = %q, want 1", task.ID)
	}

	p := b.taskPath(task.ID)
	bts, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(bts)
	for _, want := range []string{"id: 1", "title: ship widgets", "status: open", "project: p1"} {
		if !strings.Contains(content, want) {
			t.Errorf("task file missing %q:\n%s", want, content)
		}
	}
}

func TestLocalBackendPickActiveTask(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	if err := ensureDir(c.tasksDir()); err != nil {
		t.Fatalf("ensure tasks dir: %v", err)
	}
	b := &localBackend{c: c}

	assigned, err := b.AddTask("assigned open", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := b.Assign(assigned, "dev"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if got, _ := b.PickActiveTask("dev"); got == nil || got.ID != assigned.ID {
		t.Fatalf("PickActiveTask dev = %v, want task %s", got, assigned.ID)
	}

	if err := b.Claim(assigned, "dev"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if got, _ := b.PickActiveTask("dev"); got == nil || got.ID != assigned.ID {
		t.Fatalf("PickActiveTask dev in-progress = %v, want task %s", got, assigned.ID)
	}

	b.Bounce(assigned)
	if err := b.SetStatus(assigned, "done"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	unassigned, err := b.AddTask("unassigned", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if got, _ := b.PickActiveTask("other"); got == nil || got.ID != unassigned.ID {
		t.Fatalf("PickActiveTask fallback = %v, want task %s", got, unassigned.ID)
	}

	// clear task dir so there is no work
	emptyDir := t.TempDir()
	empty := &Company{Dir: emptyDir}
	emptyB := &localBackend{c: empty}
	if got, _ := emptyB.PickActiveTask("noone"); got != nil {
		t.Fatalf("PickActiveTask no work = %v, want nil", got)
	}
}
