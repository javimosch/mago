package main

import "testing"

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
