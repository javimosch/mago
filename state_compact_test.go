package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStateSections(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
		check   func(t *testing.T, s *stateSections)
	}{
		{
			name: "full state from init template",
			input: `# myco — company state

## Mission
Build the best thing.

## Shipped
Nothing yet.

## In flight
Nothing yet.

## Decisions
None yet.

## Activity log
`,
			wantNil: false,
			check: func(t *testing.T, s *stateSections) {
				if s.Mission != "Build the best thing." {
					t.Errorf("Mission = %q, want %q", s.Mission, "Build the best thing.")
				}
				if s.Shipped != "Nothing yet." {
					t.Errorf("Shipped = %q", s.Shipped)
				}
				if s.ActivityLog != "" {
					t.Errorf("ActivityLog = %q, want empty", s.ActivityLog)
				}
			},
		},
		{
			name: "with activity log entries",
			input: `# myco — company state

## Mission
Build.

## Shipped
- feature A

## In flight
- feature B

## Decisions
- use Go

## Activity log
- 2024-01-01 [cto] Added feature A
- 2024-01-02 [cto] Fixed bug
`,
			wantNil: false,
			check: func(t *testing.T, s *stateSections) {
				if s.Mission != "Build." {
					t.Errorf("Mission = %q", s.Mission)
				}
				if !strings.Contains(s.ActivityLog, "Added feature A") {
					t.Errorf("ActivityLog should contain 'Added feature A', got: %s", s.ActivityLog)
				}
				if !strings.Contains(s.ActivityLog, "Fixed bug") {
					t.Errorf("ActivityLog should contain 'Fixed bug'")
				}
			},
		},
		{
			name: "missing sections",
			input: `# myco — company state

## Activity log
- something
`,
			wantNil: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantNil: true,
		},
		{
			name: "minimal valid with single-line sections",
			input: `# c — company state

## Mission
x

## Shipped
x

## In flight
x

## Decisions
x

## Activity log
`,
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStateSections(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("parseStateSections returned non-nil: %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("parseStateSections returned nil, expected valid sections")
			}
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestCompactStateUnderThreshold(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir, Name: "testco"}
	stateFile := c.stateFile()
	content := `# testco — company state

## Mission
Test.

## Shipped
Nothing.

## In flight
Nothing.

## Decisions
None.

## Activity log
- 2024-01-01 [cto] first entry
- 2024-01-02 [cto] second entry
`
	if err := os.WriteFile(stateFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// With only 2 entries (below threshold), compaction should be a no-op.
	if err := c.compactState(); err != nil {
		t.Fatalf("compactState: %v", err)
	}

	got, _ := os.ReadFile(stateFile)
	if string(got) != content {
		t.Errorf("state file changed unexpectedly:\n--- want\n+++ got\n%s", diff(content, string(got)))
	}
}

func TestCompactStateNoFile(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir, Name: "testco"}
	if err := c.compactState(); err != nil {
		t.Fatalf("compactState on missing file: %v", err)
	}
}

func TestCompactStateMalformed(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir, Name: "testco"}
	stateFile := c.stateFile()
	content := `# just a header

some random content
`
	if err := os.WriteFile(stateFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.compactState(); err != nil {
		t.Fatalf("compactState on malformed file: %v", err)
	}
	got, _ := os.ReadFile(stateFile)
	if string(got) != content {
		t.Errorf("malformed state file was modified")
	}
}

func TestParseStateSectionsPreservesActivityLog(t *testing.T) {
	input := `# co — company state

## Mission
Do stuff

## Shipped
- X

## In flight
- Y

## Decisions
- Z

## Activity log
- entry one
- entry two
- entry three
`
	s := parseStateSections(input)
	if s == nil {
		t.Fatal("got nil")
	}
	if !strings.Contains(s.ActivityLog, "entry one") || !strings.Contains(s.ActivityLog, "entry three") {
		t.Errorf("unexpected ActivityLog: %q", s.ActivityLog)
	}
}

// TestCompactStateOverThreshold exercises the compaction path that rewrites
// STATE.md when the activity log exceeds the threshold, keeping only a small
// tail of recent entries and synthesising the structured sections via tau.
func TestCompactStateOverThreshold(t *testing.T) {
	bindir := t.TempDir()
	tauScript := filepath.Join(bindir, "tau")

	synthesis := `{"mission":"keep mission","shipped":"- shipped A","in_flight":"- in flight B","decisions":"- decide C"}`
	out, _ := json.Marshal(map[string]string{"content": synthesis})
	body := fmt.Sprintf("#!/bin/sh\ncat <<'JSON'\n%s\nJSON\n", out)
	if err := os.WriteFile(tauScript, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake tau: %v", err)
	}
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))

	c := newTestCompany(t)
	stateFile := c.stateFile()

	var logLines []string
	for i := 1; i <= stateCompactThreshold+1; i++ {
		logLines = append(logLines, fmt.Sprintf("- 2024-01-%02d [cto] did work %d", i%30+1, i))
	}

	content := fmt.Sprintf(`# test — company state

## Mission
Keep mission.

## Shipped
(none)

## In flight
(none)

## Decisions
(none)

## Activity log
%s
`, strings.Join(logLines, "\n"))
	if err := os.WriteFile(stateFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write STATE.md: %v", err)
	}

	if err := c.compactState(); err != nil {
		t.Fatalf("compactState: %v", err)
	}

	got, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read compacted STATE.md: %v", err)
	}
	s := string(got)

	if !strings.Contains(s, "## Shipped\n- shipped A") {
		t.Errorf("compacted Shipped section missing synthesis; got:\n%s", s)
	}
	if !strings.Contains(s, "## In flight\n- in flight B") {
		t.Errorf("compacted In flight section missing synthesis; got:\n%s", s)
	}
	if !strings.Contains(s, "## Decisions\n- decide C") {
		t.Errorf("compacted Decisions section missing synthesis; got:\n%s", s)
	}
	if n := strings.Count(s, "[cto] did work"); n != stateCompactKeepRecent {
		t.Errorf("expected %d recent log entries, got %d", stateCompactKeepRecent, n)
	}
}

// ---------- helpers ----------

func diff(a, b string) string {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	var out string
	max := len(al)
	if len(bl) > max {
		max = len(bl)
	}
	for i := 0; i < max; i++ {
		aa := ""
		if i < len(al) {
			aa = al[i]
		}
		bb := ""
		if i < len(bl) {
			bb = bl[i]
		}
		if aa != bb {
			out += "  -" + aa + "\n"
			out += "  +" + bb + "\n"
		}
	}
	return out
}

func TestAppendToSection(t *testing.T) {
	dir, _ := os.MkdirTemp("", "mago-sec")
	defer os.RemoveAll(dir)
	c := &Company{Dir: dir, Name: "co"}
	os.WriteFile(c.stateFile(), []byte("# co — company state\n\n## Shipped\n(nothing yet)\n\n## In flight\n(nothing yet)\n\n## Activity log\n- existing entry\n"), 0o644)

	c.appendToSection("Shipped", "- 2026 #1 first plugin")
	c.appendToSection("Shipped", "- 2026 #2 second plugin")
	c.appendToSection("Shipped", "- 2026 #1 first plugin") // dup -> ignored

	out, _ := os.ReadFile(c.stateFile())
	s := string(out)
	if !strings.Contains(s, "## In flight\n(nothing yet)") {
		// "In flight" placeholder must remain untouched
		t.Errorf("In flight section was disturbed:\n%s", s)
	}
	if strings.Count(s, "first plugin") != 1 {
		t.Errorf("dup not deduped:\n%s", s)
	}
	if !strings.Contains(s, "## Shipped\n- 2026 #1 first plugin\n- 2026 #2 second plugin\n") {
		t.Errorf("Shipped not populated correctly:\n%s", s)
	}
	if !strings.Contains(s, "## Activity log\n- existing entry") {
		t.Errorf("Activity log disturbed:\n%s", s)
	}
}

// TestSynthesizeState exercises the LLM state-synthesis path with a fake tau binary.
func TestSynthesizeState(t *testing.T) {
	bindir := t.TempDir()
	script := filepath.Join(bindir, "tau")
	writeFakeTau := func(content string) {
		// tauComplete returns the last JSON object with a non-empty "content" field.
		// The content here is itself a JSON string that synthesizeState unmarshals.
		body := fmt.Sprintf("#!/bin/sh\ncat <<'JSON'\n%s\nJSON\n", content)
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatalf("write fake tau: %v", err)
		}
	}
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))

	c := newTestCompany(t)
	base := &stateSections{
		Mission:   "original mission",
		Shipped:   "original shipped",
		InFlight:  "original in flight",
		Decisions: "original decisions",
	}

	cases := []struct {
		name    string
		content string
		check   func(t *testing.T, got *stateSections)
	}{
		{
			name:    "updates all sections",
			content: `{"content":"{\"mission\":\"keep mission\",\"shipped\":\"- shipped A\",\"in_flight\":\"- in flight B\",\"decisions\":\"- decide C\"}"}`,
			check: func(t *testing.T, got *stateSections) {
				if got.Mission != "keep mission" {
					t.Errorf("Mission = %q, want %q", got.Mission, "keep mission")
				}
				if got.Shipped != "- shipped A" {
					t.Errorf("Shipped = %q, want %q", got.Shipped, "- shipped A")
				}
				if got.InFlight != "- in flight B" {
					t.Errorf("InFlight = %q, want %q", got.InFlight, "- in flight B")
				}
				if got.Decisions != "- decide C" {
					t.Errorf("Decisions = %q, want %q", got.Decisions, "- decide C")
				}
			},
		},
		{
			name:    "falls back to base for empty fields",
			content: `{"content":"{}"}`,
			check: func(t *testing.T, got *stateSections) {
				if got.Mission != "original mission" {
					t.Errorf("Mission = %q, want %q", got.Mission, "original mission")
				}
				if got.Shipped != "original shipped" {
					t.Errorf("Shipped = %q, want %q", got.Shipped, "original shipped")
				}
				if got.InFlight != "original in flight" {
					t.Errorf("InFlight = %q, want %q", got.InFlight, "original in flight")
				}
				if got.Decisions != "original decisions" {
					t.Errorf("Decisions = %q, want %q", got.Decisions, "original decisions")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeFakeTau(tc.content)
			got, err := c.synthesizeState(base, []string{"- 2024-01-01 [cto] did work"})
			if err != nil {
				t.Fatalf("synthesizeState: %v", err)
			}
			tc.check(t, got)
		})
	}
}
