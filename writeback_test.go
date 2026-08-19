package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProgressNote(t *testing.T) {
	cases := []struct {
		status, summary, next, want string
	}{
		{
			status:  "done",
			summary: "shipped the feature",
			next:    "open champagne",
			want:    "**done**\n\nshipped the feature\n\n_Next:_ open champagne",
		},
		{
			status:  "blocked",
			summary: "stuck on a dependency",
			next:    "",
			want:    "**blocked**\n\nstuck on a dependency",
		},
		{
			status:  "in progress",
			summary: "  made progress  ",
			next:    "   ",
			want:    "**in progress**\n\nmade progress",
		},
	}
	for _, tc := range cases {
		got := progressNote(tc.status, tc.summary, tc.next)
		if got != tc.want {
			t.Errorf("progressNote(%q, %q, %q)\n got: %q\nwant: %q", tc.status, tc.summary, tc.next, got, tc.want)
		}
	}
}

func TestProgressEntry(t *testing.T) {
	got := progressEntry("2026-08-14T10-00-00Z", "dev", "did a thing")
	want := "\n\n### 2026-08-14T10-00-00Z [dev]\ndid a thing\n"
	if got != want {
		t.Errorf("progressEntry(...) = %q, want %q", got, want)
	}
}

func TestWritebackAppendState(t *testing.T) {
	c := &Company{Dir: t.TempDir()}
	c.appendState("first line")
	c.appendState("second line")

	b, err := os.ReadFile(c.stateFile())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "# Company state") {
		t.Errorf("missing header")
	}
	if !strings.Contains(content, "first line") || !strings.Contains(content, "second line") {
		t.Errorf("missing lines: %s", content)
	}
}

func TestWritebackAppendToSection(t *testing.T) {
	c := &Company{Dir: t.TempDir()}
	state := "# Company state\n\n## Shipped\n(nothing yet)\n\n## Activity log\n\n"
	if err := os.WriteFile(c.stateFile(), []byte(state), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	line := "- 2026-08-14 #1 shipped a thing"
	c.appendToSection("Shipped", line)

	b, err := os.ReadFile(c.stateFile())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, line) {
		t.Errorf("section missing new line: %s", content)
	}
	if strings.Contains(content, "(nothing yet)") {
		t.Errorf("placeholder was not replaced: %s", content)
	}

	c.appendToSection("Shipped", line)
	b, err = os.ReadFile(c.stateFile())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Count(string(b), line) != 1 {
		t.Errorf("duplicate line added")
	}

	c.appendToSection("Missing", "- should not appear")
	b, err = os.ReadFile(c.stateFile())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(b), "should not appear") {
		t.Errorf("line added to nonexistent section")
	}
}

func TestWritebackWriteJournal(t *testing.T) {
	c := &Company{Dir: t.TempDir()}
	a := &Agent{Name: "dev"}
	task := &Task{ID: "42", Title: "write tests"}
	r := &Reflection{
		Summary:       "wrote a run journal",
		StateDelta:    "state updated",
		TaskStatus:    "in_progress",
		Lessons:       []Lesson{{Skill: "go", Note: "keep tests small"}},
		Next:          "cover more helpers",
		CadenceSignal: "working",
	}
	c.writeJournal(a, task, r, "2026-08-19T10-00-00Z")

	dir := filepath.Join(c.runsDir(), "dev")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 run files, got %d", len(entries))
	}

	jsonPath := filepath.Join(dir, "2026-08-19T10-00-00Z.json")
	b, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("ReadFile json: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, `"task_id": "42"`) {
		t.Errorf("json missing task_id: %s", content)
	}
	if !strings.Contains(content, `"agent": "dev"`) {
		t.Errorf("json missing agent: %s", content)
	}
	if !strings.Contains(content, `"summary": "wrote a run journal"`) {
		t.Errorf("json missing summary: %s", content)
	}

	mdPath := filepath.Join(dir, "2026-08-19T10-00-00Z.md")
	b, err = os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("ReadFile md: %v", err)
	}
	content = string(b)
	if !strings.Contains(content, "**Task:** #42 write tests") {
		t.Errorf("md missing task heading: %s", content)
	}
	if !strings.Contains(content, "**Status:** in_progress") {
		t.Errorf("md missing status: %s", content)
	}
}

func TestWritebackAppendSkill(t *testing.T) {
	c := &Company{Dir: t.TempDir()}
	c.appendSkill("Go Conventions", "use gofmt before commit", "dev", "2026-08-19T10-00-00Z")

	p := filepath.Join(c.skillsDir(), "go-conventions", "SKILL.md")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "name: go-conventions") {
		t.Errorf("missing frontmatter name: %s", content)
	}
	if !strings.Contains(content, "use gofmt before commit") {
		t.Errorf("missing note: %s", content)
	}

	idx, err := os.ReadFile(c.skillsIndex())
	if err != nil {
		t.Fatalf("ReadFile index: %v", err)
	}
	if !strings.Contains(string(idx), "go-conventions") || !strings.Contains(string(idx), "use gofmt before commit") {
		t.Errorf("index missing entry: %s", string(idx))
	}

	// duplicate note should not be recorded again
	c.appendSkill("Go Conventions", "use gofmt before commit", "dev", "2026-08-19T10-00-00Z")
	b, err = os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	bullet := "- 2026-08-19T10-00-00Z [dev] use gofmt before commit"
	if strings.Count(string(b), bullet) != 1 {
		t.Errorf("duplicate note added")
	}

	// a new note appends to the same skill file
	c.appendSkill("Go Conventions", "keep files under 500 LOC", "dev", "2026-08-19T10-00-00Z")
	b, err = os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(b), "- 2026-08-19T10-00-00Z [dev] keep files under 500 LOC") {
		t.Errorf("new note missing: %s", string(b))
	}
}

func TestWritebackAppendIndexLine(t *testing.T) {
	c := &Company{Dir: t.TempDir()}
	c.appendIndexLine("testing", "write unit tests first")

	b, err := os.ReadFile(c.skillsIndex())
	if err != nil {
		t.Fatalf("ReadFile index: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "testing") || !strings.Contains(content, "write unit tests first") {
		t.Errorf("index missing first entry: %s", content)
	}

	c.appendIndexLine("linting", "run gofmt before commit")
	b, err = os.ReadFile(c.skillsIndex())
	if err != nil {
		t.Fatalf("ReadFile index: %v", err)
	}
	if !strings.Contains(string(b), "linting") || !strings.Contains(string(b), "run gofmt before commit") {
		t.Errorf("index missing second entry: %s", string(b))
	}
}

func TestApplyTaskStatus(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)
	c := &Company{Dir: dir}
	c.tasks = &localBackend{c: c}

	task, err := c.tasks.AddTask("test task", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	cases := []struct {
		status     string
		wantStatus string
		bodyHas    string
	}{
		{"done", "done", "✅ done"},
		{"already_done", "done", "already done"},
		{"blocked", "blocked", "⛔ blocked"},
		{"reassign", "open", "↩ reassign"},
		{"needs_human", "needs_human", "NEEDS HUMAN"},
		{"in_progress", "in_progress", "… in progress"},
	}

	for _, tc := range cases {
		task, err = c.tasks.FindTask(task.ID)
		if err != nil {
			t.Fatalf("FindTask: %v", err)
		}
		r := &Reflection{
			TaskStatus:   tc.status,
			Summary:      "did the thing",
			Next:         "next step",
			HitlQuestion: "help",
		}
		c.applyTaskStatus(task, &Agent{Name: "dev"}, r)
		if task.Status != tc.wantStatus {
			t.Errorf("status %q -> got %q, want %q", tc.status, task.Status, tc.wantStatus)
		}
		if !strings.Contains(task.Body, tc.bodyHas) {
			t.Errorf("status %q body missing %q:\n%s", tc.status, tc.bodyHas, task.Body)
		}
	}

	// needs_human should create an inbox file.
	inboxPath := filepath.Join(c.inboxDir(), "task-"+task.ID+".md")
	if _, err := os.Stat(inboxPath); err != nil {
		t.Errorf("needs_human should create inbox file: %v", err)
	}
}

func TestWritebackWriteRawFailure(t *testing.T) {
	c := &Company{Dir: t.TempDir()}
	a := &Agent{Name: "dev"}
	task := &Task{ID: "7", Title: "failing task"}
	c.writeRawFailure(a, task, "raw model output")

	dir := filepath.Join(c.runsDir(), "dev")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one raw failure file, got %d", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), "-RAW.txt") {
		t.Fatalf("unexpected file: %s", entries[0].Name())
	}

	b, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(b) != "raw model output" {
		t.Errorf("content = %q, want %q", string(b), "raw model output")
	}
}
