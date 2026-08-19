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
