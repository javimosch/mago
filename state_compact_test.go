package main

import (
	"os"
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
