package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetMission(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir, Name: "co"}
	os.WriteFile(c.stateFile(), []byte("# co — company state\n\n## Mission\n(Set by the CEO. Edit me.)\n\n## Shipped\n- thing one\n\n## In flight\n(nothing yet)\n\n## Decisions\n(none yet)\n\n## Activity log\n- entry\n"), 0o644)

	c.setMission("Ship a delightful CLI.")
	got, _ := os.ReadFile(c.stateFile())
	s := string(got)
	if c.missionText() != "Ship a delightful CLI." {
		t.Errorf("mission not set, got %q", c.missionText())
	}
	if !strings.Contains(s, "## Shipped\n- thing one") {
		t.Errorf("Shipped section disturbed:\n%s", s)
	}
	if !strings.Contains(s, "## Activity log\n- entry") {
		t.Errorf("Activity log disturbed:\n%s", s)
	}
	if strings.Contains(s, "Set by the CEO") {
		t.Errorf("old placeholder mission not removed:\n%s", s)
	}
}

func TestCleanTaskTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"**Add a --color flag and --help docs**", "Add a --color flag and --help docs"},
		{"- Add time-of-day greeting modes", "Add time-of-day greeting modes"},
		{"1. Initialize the project skeleton", "Initialize the project skeleton"},
		{"2) Write tests for greet()", "Write tests for greet()"},
		{`"Polish the README"`, "Polish the README"},
		{"* **Support a config file**", "Support a config file"},
		{"2FA support for the login flow", "2FA support for the login flow"}, // leading digit must survive
		{"   ", ""},  // empty
		{"- ok", ""}, // too short after cleaning
	}
	for _, c := range cases {
		if got := cleanTaskTitle(c.in); got != c.want {
			t.Errorf("cleanTaskTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDuplicateTitle(t *testing.T) {
	open := []string{"Add a --shout flag to greet-cli"}
	if !duplicateTitle("add a --shout flag to greet-cli", open) {
		t.Error("exact (case-insensitive) match should be a duplicate")
	}
	if !duplicateTitle("shout flag", open) {
		t.Error("substring should be a duplicate")
	}
	if duplicateTitle("Add a --whisper flag", open) {
		t.Error("distinct title should not be a duplicate")
	}
}

func TestExtractProjectName(t *testing.T) {
	projects := map[string]projConf{
		"supercli": {Repo: "javimosch/supercli"},
		"machin":   {Repo: "javimosch/machin"},
		"mago":     {Repo: "javimosch/mago"},
	}
	cases := []struct {
		title, want string
	}{
		// With project prefix
		{"supercli: unit tests for mcp-manager.js (npm test)", "supercli"},
		{"SUPERCLI: unit tests (case-insensitive)", "supercli"},
		{"machin: GET /api/repos endpoint", "machin"},

		// Without project prefix or no colon
		{"Add a --color flag", ""},
		{"Something: unrelated: has colons", ""},
		{"Write tests for greet()", ""},

		// Edge cases
		{": something", ""},                             // empty prefix
		{"unknown: something", ""},                      // prefix not in projects
		{"supercli :has space after colon", "supercli"}, // space is trimmed
	}
	for _, c := range cases {
		got := extractProjectName(c.title, projects)
		if got != c.want {
			t.Errorf("extractProjectName(%q) = %q, want %q", c.title, got, c.want)
		}
	}
}

func TestProactiveMaxPerCycle(t *testing.T) {
	cases := []struct {
		env  string
		want int
	}{
		{"", 2},             // default
		{"1", 1},            // override
		{"5", 5},            // larger override
		{"0", 2},            // zero falls back to default
		{"not-a-number", 2}, // invalid falls back to default
		{"  3  ", 3},        // trimmed whitespace
		{" ", 2},            // whitespace-only treated as unset
	}
	for _, c := range cases {
		t.Setenv("MAGO_PROACTIVE_MAX", c.env)
		if got := proactiveMaxPerCycle(); got != c.want {
			t.Errorf("proactiveMaxPerCycle() with MAGO_PROACTIVE_MAX=%q = %d, want %d", c.env, got, c.want)
		}
	}
}

func TestShippedText(t *testing.T) {
	c := newTestCompany(t)

	// Missing STATE.md -> empty
	if got := c.shippedText(); got != "" {
		t.Errorf("missing STATE.md: got %q, want empty", got)
	}

	placeholder := "# co\n\n## Mission\n(Set by the CEO. Edit me.)\n\n## Shipped\n(nothing yet)\n\n## In flight\n(nothing yet)\n\n## Decisions\n(none yet)\n\n## Activity log\n- entry\n"
	os.WriteFile(c.stateFile(), []byte(placeholder), 0o644)
	if got := c.shippedText(); got != "" {
		t.Errorf("placeholder Shipped: got %q, want empty", got)
	}

	real := "# co\n\n## Mission\n(Set by the CEO. Edit me.)\n\n## Shipped\n- landed onboarding flow\n- fixed login redirect\n\n## In flight\n(nothing yet)\n\n## Decisions\n(none yet)\n\n## Activity log\n- entry\n"
	os.WriteFile(c.stateFile(), []byte(real), 0o644)
	if got := c.shippedText(); got != "- landed onboarding flow\n- fixed login redirect" {
		t.Errorf("real Shipped: got %q, want %q", got, "- landed onboarding flow\n- fixed login redirect")
	}
}

func TestMissionText(t *testing.T) {
	c := newTestCompany(t)

	// Missing STATE.md -> empty
	if got := c.missionText(); got != "" {
		t.Errorf("missing STATE.md: got %q, want empty", got)
	}

	placeholder := "# co\n\n## Mission\n(Set by the CEO. Edit me.)\n\n## Shipped\n(none)\n\n## In flight\n(none)\n\n## Decisions\n(none)\n\n## Activity log\n- entry\n"
	os.WriteFile(c.stateFile(), []byte(placeholder), 0o644)
	if got := c.missionText(); got != "" {
		t.Errorf("placeholder Mission: got %q, want empty", got)
	}

	real := "# co\n\n## Mission\nShip a delightful CLI.\n\n## Shipped\n(none)\n\n## In flight\n(none)\n\n## Decisions\n(none)\n\n## Activity log\n- entry\n"
	os.WriteFile(c.stateFile(), []byte(real), 0o644)
	if got := c.missionText(); got != "Ship a delightful CLI." {
		t.Errorf("real Mission: got %q, want %q", got, "Ship a delightful CLI.")
	}

	// Leading/trailing whitespace is trimmed.
	trimmed := "# co\n\n## Mission\n  Ship it.  \n\n## Shipped\n(none)\n\n## In flight\n(none)\n\n## Decisions\n(none)\n\n## Activity log\n- entry\n"
	os.WriteFile(c.stateFile(), []byte(trimmed), 0o644)
	if got := c.missionText(); got != "Ship it." {
		t.Errorf("trimmed Mission: got %q, want %q", got, "Ship it.")
	}
}

func TestPlannerAgent(t *testing.T) {
	// No agents -> nil
	c := newTestCompany(t)
	if got := c.plannerAgent(); got != nil {
		t.Errorf("empty roster: got %v, want nil", got)
	}

	// Non-planner agents -> nil
	c = newTestCompany(t)
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Developer\n---\nYou code.")
	if got := c.plannerAgent(); got != nil {
		t.Errorf("non-planner: got %v, want nil", got)
	}

	// Planner found and returned
	c = newTestCompany(t)
	writeAgentFile(t, c, "hop", "---\nname: hop\ntitle: Head of Product\nplans: true\nprovider: deepseek\n---\nYou plan.")
	got := c.plannerAgent()
	if got == nil {
		t.Fatal("expected planner agent, got nil")
	}
	if got.Name != "hop" || got.Title != "Head of Product" {
		t.Errorf("planner identity: got %q / %q, want %q / %q", got.Name, got.Title, "hop", "Head of Product")
	}

	// Model override from env is applied
	c = newTestCompany(t)
	writeAgentFile(t, c, "hop", "---\nname: hop\ntitle: Head of Product\nplans: true\nprovider: deepseek\n---\nYou plan.")
	t.Setenv("MAGO_PROVIDER", "opencode-go")
	t.Setenv("MAGO_MODEL", "gpt-4")
	got = c.plannerAgent()
	if got == nil {
		t.Fatal("expected planner agent, got nil")
	}
	if got.Provider != "opencode-go" || got.Model != "gpt-4" {
		t.Errorf("model override: got provider=%q model=%q, want opencode-go / gpt-4", got.Provider, got.Model)
	}
}

// TestProposeBacklog exercises the planner's early-exit paths and a successful filing
// cycle using a fake tau binary so the test runs without a real provider.
func TestProposeBacklog(t *testing.T) {
	t.Run("no focus", func(t *testing.T) {
		c := newTestCompany(t)
		c.tasks = &localBackend{c: c}
		if got := c.proposeBacklog(); got != 0 {
			t.Errorf("proposeBacklog() with no focus = %d, want 0", got)
		}
	})

	t.Run("active at cap", func(t *testing.T) {
		c := newTestCompany(t)
		c.tasks = &localBackend{c: c}
		mission := "# co\n\n## Mission\nShip things.\n\n## Shipped\n(none)\n\n## In flight\n(none)\n\n## Decisions\n(none)\n\n## Activity log\n"
		if err := os.WriteFile(c.stateFile(), []byte(mission), 0o644); err != nil {
			t.Fatalf("write state: %v", err)
		}
		for i := 0; i < 3; i++ {
			if _, err := c.tasks.AddTask(fmt.Sprintf("active task %d", i), ""); err != nil {
				t.Fatalf("add task: %v", err)
			}
		}
		if got := c.proposeBacklog(); got != 0 {
			t.Errorf("proposeBacklog() at cap = %d, want 0", got)
		}
	})

	t.Run("no planner", func(t *testing.T) {
		c := newTestCompany(t)
		c.tasks = &localBackend{c: c}
		mission := "# co\n\n## Mission\nShip things.\n\n## Shipped\n(none)\n\n## In flight\n(none)\n\n## Decisions\n(none)\n\n## Activity log\n"
		if err := os.WriteFile(c.stateFile(), []byte(mission), 0o644); err != nil {
			t.Fatalf("write state: %v", err)
		}
		if got := c.proposeBacklog(); got != 0 {
			t.Errorf("proposeBacklog() with no planner = %d, want 0", got)
		}
	})

	t.Run("proposes up to per-cycle cap", func(t *testing.T) {
		bindir := t.TempDir()
		script := filepath.Join(bindir, "tau")
		// tauComplete returns the last JSON object with a "content" field.
		// The content string uses JSON \n escapes, which Unmarshal turns into real newlines.
		body := "#!/bin/sh\nprintf '%s\\n' '{\"content\":\"Implement the login flow\\nAdd password reset\"}'\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatalf("write fake tau: %v", err)
		}
		t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))

		c := newTestCompany(t)
		c.tasks = &localBackend{c: c}
		mission := "# co\n\n## Mission\nShip a delightful CLI.\n\n## Shipped\n(none)\n\n## In flight\n(none)\n\n## Decisions\n(none)\n\n## Activity log\n"
		if err := os.WriteFile(c.stateFile(), []byte(mission), 0o644); err != nil {
			t.Fatalf("write state: %v", err)
		}
		writeAgentFile(t, c, "hop", "---\nname: hop\ntitle: Head of Product\nplans: true\nprovider: deepseek\n---\nYou plan.")

		got := c.proposeBacklog()
		if got != 2 {
			t.Errorf("proposeBacklog() = %d, want 2", got)
		}
		ts, err := c.tasks.ListTasks()
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		if len(ts) != 2 {
			t.Errorf("filed %d tasks, want 2", len(ts))
		}
	})
}

// TestProposeBacklog_FocusCompleteHoldsWithActive verifies that when the planner
// reports FOCUS_COMPLETE but there are still active tasks, proposeBacklog returns
// 0 without filing new tasks and prints a holding message.
func TestProposeBacklog_FocusCompleteHoldsWithActive(t *testing.T) {
	bindir := t.TempDir()
	script := filepath.Join(bindir, "tau")
	body := "#!/bin/sh\nprintf '%s\\n' '{\"content\":\"FOCUS_COMPLETE\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake tau: %v", err)
	}
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}

	mission := "# co\n\n## Mission\nShip a delightful CLI.\n\n## Shipped\n(none)\n\n## In flight\n(none)\n\n## Decisions\n(none)\n\n## Activity log\n"
	if err := os.WriteFile(c.stateFile(), []byte(mission), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	writeAgentFile(t, c, "hop", "---\nname: hop\ntitle: Head of Product\nplans: true\nprovider: deepseek\n---\nYou plan.")

	if _, err := c.tasks.AddTask("active task", ""); err != nil {
		t.Fatalf("add task: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	got := c.proposeBacklog()
	w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)

	if got != 0 {
		t.Errorf("proposeBacklog() = %d, want 0", got)
	}
	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(ts) != 1 {
		t.Errorf("proposeBacklog filed %d tasks, want 0", len(ts)-1)
	}
	if !strings.Contains(string(out), "focus complete but") || !strings.Contains(string(out), "still active") {
		t.Errorf("stderr should report focus held due to active tasks, got:\n%s", string(out))
	}
}

func TestOrNone(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "(none)"},
		{"   ", "(none)"},
		{"\t\n", "(none)"},
		{"hello", "hello"},
		{"  hello  ", "  hello  "},
	}
	for _, c := range cases {
		if got := orNone(c.in); got != c.want {
			t.Errorf("orNone(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
