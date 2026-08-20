package main

import (
	"os"
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
