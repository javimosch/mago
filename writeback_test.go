package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritebackApplyTaskStatus(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	if err := ensureDir(c.tasksDir()); err != nil {
		t.Fatalf("ensure tasks dir: %v", err)
	}
	c.tasks = &localBackend{c: c}

	state := "# Company state\n\n## Shipped\n(nothing yet)\n\n## Activity log\n\n"
	if err := os.WriteFile(c.stateFile(), []byte(state), 0o644); err != nil {
		t.Fatalf("WriteFile state: %v", err)
	}

	agent := &Agent{Name: "dev"}

	t.Run("done", func(t *testing.T) {
		task, err := c.tasks.AddTask("ship it", "")
		if err != nil {
			t.Fatalf("AddTask: %v", err)
		}
		r := &Reflection{Summary: "shipped the feature", TaskStatus: "done", Next: "monitor"}
		c.applyTaskStatus(task, agent, r)
		if task.Status != "done" {
			t.Errorf("status = %q, want done", task.Status)
		}
		if !strings.Contains(task.Body, "done") {
			t.Errorf("progress body missing done: %s", task.Body)
		}
		b, _ := os.ReadFile(c.stateFile())
		if !strings.Contains(string(b), "ship it") {
			t.Errorf("Shipped section missing task: %s", b)
		}
	})

	t.Run("needs_human", func(t *testing.T) {
		task, err := c.tasks.AddTask("hitl", "")
		if err != nil {
			t.Fatalf("AddTask: %v", err)
		}
		r := &Reflection{Summary: "need input", TaskStatus: "needs_human", HitlQuestion: "what next?"}
		c.applyTaskStatus(task, agent, r)
		if task.Status != "needs_human" {
			t.Errorf("status = %q, want needs_human", task.Status)
		}
		if !strings.Contains(task.Body, "NEEDS HUMAN") {
			t.Errorf("progress body missing HITL: %s", task.Body)
		}
	})

	t.Run("blocked", func(t *testing.T) {
		task, err := c.tasks.AddTask("blocked task", "")
		if err != nil {
			t.Fatalf("AddTask: %v", err)
		}
		r := &Reflection{Summary: "stuck on dep", TaskStatus: "blocked", Next: "unblock"}
		c.applyTaskStatus(task, agent, r)
		if task.Status != "blocked" {
			t.Errorf("status = %q, want blocked", task.Status)
		}
	})

	t.Run("reassign", func(t *testing.T) {
		task, err := c.tasks.AddTask("reassign", "")
		if err != nil {
			t.Fatalf("AddTask: %v", err)
		}
		if err := c.tasks.Claim(task, "other"); err != nil {
			t.Fatalf("Claim: %v", err)
		}
		r := &Reflection{Summary: "not my role", TaskStatus: "reassign", Next: ""}
		c.applyTaskStatus(task, agent, r)
		if task.Status != "open" || task.Assignee != "" {
			t.Errorf("after reassign: status=%q assignee=%q", task.Status, task.Assignee)
		}
	})

	t.Run("default_in_progress", func(t *testing.T) {
		task, err := c.tasks.AddTask("ongoing", "")
		if err != nil {
			t.Fatalf("AddTask: %v", err)
		}
		r := &Reflection{Summary: "working", TaskStatus: "in_progress", Next: "continue"}
		c.applyTaskStatus(task, agent, r)
		if task.Status != "in_progress" {
			t.Errorf("status = %q, want in_progress", task.Status)
		}
	})
}

func TestWritebackWriteJournal(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	a := &Agent{Name: "dev"}
	task := &Task{ID: "1", Title: "demo"}
	r := &Reflection{Summary: "s", StateDelta: "d", TaskStatus: "done", Next: "n", CadenceSignal: "idle"}
	ts := "2026-08-14T10-00-00Z"
	c.writeJournal(a, task, r, ts)

	runDir := filepath.Join(c.runsDir(), a.Name)
	files, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 run files, got %d", len(files))
	}

	jsonPath := filepath.Join(runDir, ts+".json")
	b, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("ReadFile json: %v", err)
	}
	if !strings.Contains(string(b), `"task_id": "1"`) {
		t.Errorf("journal json missing task_id: %s", b)
	}

	mdPath := filepath.Join(runDir, ts+".md")
	b, err = os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("ReadFile md: %v", err)
	}
	if !strings.Contains(string(b), "demo") {
		t.Errorf("journal md missing title: %s", b)
	}
}

func TestWritebackAppendSkillAndIndex(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	note := "always rebase before push"
	c.appendSkill("git-tips", note, "dev", "2026-08-14T10-00-00Z")

	skillPath := filepath.Join(c.skillsDir(), "git-tips", "SKILL.md")
	b, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile skill: %v", err)
	}
	if !strings.Contains(string(b), note) {
		t.Errorf("skill missing note: %s", b)
	}

	idx, err := os.ReadFile(c.skillsIndex())
	if err != nil {
		t.Fatalf("ReadFile index: %v", err)
	}
	if !strings.Contains(string(idx), "git-tips") {
		t.Errorf("index missing skill: %s", idx)
	}

	// duplicate note should be skipped
	c.appendSkill("git-tips", note, "dev", "2026-08-14T10-00-00Z")
	b, err = os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile skill after dup: %v", err)
	}
	if strings.Count(string(b), "## Notes") == 0 {
		t.Fatalf("skill missing Notes section")
	}
	body := string(b)
	notesIdx := strings.Index(body, "## Notes")
	notes := body[notesIdx:]
	if strings.Count(notes, "\n-") != 1 {
		t.Errorf("expected one note bullet after dup, got:\n%s", notes)
	}

	// a new note should append
	c.appendSkill("git-tips", "never force push", "dev", "2026-08-14T10-00-00Z")
	b, err = os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile skill after new: %v", err)
	}
	body = string(b)
	notesIdx = strings.Index(body, "## Notes")
	notes = body[notesIdx:]
	if strings.Count(notes, "\n-") != 2 {
		t.Errorf("expected two note bullets, got:\n%s", notes)
	}
}

func TestWritebackRawFailure(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	a := &Agent{Name: "dev"}
	task := &Task{ID: "1", Title: "demo"}
	c.writeRawFailure(a, task, "raw failure text")

	runDir := filepath.Join(c.runsDir(), a.Name)
	files, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 raw file, got %d", len(files))
	}
	b, err := os.ReadFile(filepath.Join(runDir, files[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(b), "raw failure text") {
		t.Errorf("raw file missing text: %s", b)
	}
}
