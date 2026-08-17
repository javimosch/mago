package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func newTestWritebackCompany(t *testing.T) *Company {
	t.Helper()
	dir := t.TempDir()
	c := &Company{Dir: dir, Name: "acme"}
	for _, d := range []string{c.tasksDir(), c.inboxDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("create dir %s: %v", d, err)
		}
	}
	c.tasks = &localBackend{c: c}
	if err := os.WriteFile(c.stateFile(), []byte("# acme\n\n## Shipped\n(none)\n\n## Activity log\n\n"), 0o644); err != nil {
		t.Fatalf("write STATE.md: %v", err)
	}
	return c
}

func TestProgressNote(t *testing.T) {
	cases := []struct {
		name    string
		status  string
		summary string
		next    string
		want    string
	}{
		{
			name:    "status and summary only",
			status:  "done",
			summary: "finished the thing",
			want:    "**done**\n\nfinished the thing",
		},
		{
			name:    "status summary and next",
			status:  "in progress",
			summary: "made progress",
			next:    "do more",
			want:    "**in progress**\n\nmade progress\n\n_Next:_ do more",
		},
		{
			name:    "next is flattened to one line",
			status:  "blocked",
			summary: "stuck",
			next:    "ask\nfor\nhelp",
			want:    "**blocked**\n\nstuck\n\n_Next:_ ask for help",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := progressNote(c.status, c.summary, c.next)
			if got != c.want {
				t.Errorf("progressNote(%q, %q, %q) = %q, want %q", c.status, c.summary, c.next, got, c.want)
			}
		})
	}
}

func TestApplyTaskStatus(t *testing.T) {
	agent := &Agent{Name: "cto"}

	t.Run("done updates task and ships section", func(t *testing.T) {
		c := newTestWritebackCompany(t)
		task, _ := c.tasks.AddTask("ship it", "")
		r := &Reflection{Summary: "implemented", TaskStatus: "done", Next: "verify"}

		c.applyTaskStatus(task, agent, r)

		if task.Status != "done" {
			t.Fatalf("want status done, got %q", task.Status)
		}
		if !strings.Contains(task.Body, "✅ done") {
			t.Fatalf("body missing done marker: %s", task.Body)
		}
		state := readFileOr(c.stateFile(), "")
		if !strings.Contains(state, fmt.Sprintf("#%s", task.ID)) {
			t.Fatalf("state file missing shipped entry: %s", state)
		}
	})

	t.Run("already_done marks complete without shipping", func(t *testing.T) {
		c := newTestWritebackCompany(t)
		task, _ := c.tasks.AddTask("already shipped", "")
		r := &Reflection{Summary: "was already merged", TaskStatus: "already_done"}

		c.applyTaskStatus(task, agent, r)

		if task.Status != "done" || !strings.Contains(task.Body, "✅ already done") {
			t.Fatalf("unexpected task: %+v, body: %s", task, task.Body)
		}
	})

	t.Run("needs_human raises HITL", func(t *testing.T) {
		c := newTestWritebackCompany(t)
		task, _ := c.tasks.AddTask("need input", "")
		r := &Reflection{Summary: "unsure", TaskStatus: "needs_human", HitlQuestion: "which api?"}

		c.applyTaskStatus(task, agent, r)

		if task.Status != "needs_human" {
			t.Fatalf("want status needs_human, got %q", task.Status)
		}
		if !strings.Contains(task.Body, "NEEDS HUMAN") {
			t.Fatalf("body missing HITL marker: %s", task.Body)
		}
		pending, _ := c.tasks.PendingHITL()
		if len(pending) != 1 {
			t.Fatalf("want 1 pending HITL, got %d", len(pending))
		}
	})

	t.Run("reassign bounces the task", func(t *testing.T) {
		c := newTestWritebackCompany(t)
		task, _ := c.tasks.AddTask("wrong role", "")
		_ = c.tasks.Assign(task, "cto")
		r := &Reflection{Summary: "not my role", TaskStatus: "reassign"}

		c.applyTaskStatus(task, agent, r)

		if task.Status != "open" || task.Assignee != "" {
			t.Fatalf("want task bounced, got %+v", task)
		}
		if !strings.Contains(task.Body, "↩ reassign") {
			t.Fatalf("body missing reassign marker: %s", task.Body)
		}
	})

	t.Run("blocked records blocker", func(t *testing.T) {
		c := newTestWritebackCompany(t)
		task, _ := c.tasks.AddTask("stuck", "")
		r := &Reflection{Summary: "waiting", TaskStatus: "blocked", Next: "get access"}

		c.applyTaskStatus(task, agent, r)

		if task.Status != "blocked" || !strings.Contains(task.Body, "⛔ blocked") {
			t.Fatalf("unexpected task: %+v, body: %s", task, task.Body)
		}
	})

	t.Run("default is in_progress", func(t *testing.T) {
		c := newTestWritebackCompany(t)
		task, _ := c.tasks.AddTask("keep going", "")
		r := &Reflection{Summary: "worked", TaskStatus: "in_progress", Next: "more"}

		c.applyTaskStatus(task, agent, r)

		if task.Status != "in_progress" || !strings.Contains(task.Body, "… in progress") {
			t.Fatalf("unexpected task: %+v, body: %s", task, task.Body)
		}
	})
}

func TestAppendState(t *testing.T) {
	c := newTestWritebackCompany(t)

	c.appendState("- 2026-01-01 [cto] first")
	c.appendState("- 2026-01-02 [cto] second")

	state := readFileOr(c.stateFile(), "")
	if !strings.Contains(state, "first") || !strings.Contains(state, "second") {
		t.Fatalf("state missing appended lines: %s", state)
	}
}

func TestAppendToSectionDedupe(t *testing.T) {
	c := newTestWritebackCompany(t)

	c.appendToSection("Shipped", "- shipped first")
	c.appendToSection("Shipped", "- shipped second")
	c.appendToSection("Shipped", "- shipped first") // duplicate, should be ignored

	state := readFileOr(c.stateFile(), "")
	if strings.Count(state, "shipped first") != 1 {
		t.Fatalf("duplicate not deduped: %s", state)
	}
	if !strings.Contains(state, "shipped second") {
		t.Fatalf("second entry missing: %s", state)
	}
	if strings.Contains(state, "(none)") {
		t.Fatalf("placeholder not replaced: %s", state)
	}
}
