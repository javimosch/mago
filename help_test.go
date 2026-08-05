package main

import (
	"strings"
	"testing"
)

func TestWantsHelp(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"add", "a title"}, false},
		{[]string{"-h"}, true},
		{[]string{"--help"}, true},
		{[]string{"add", "x", "--help"}, true},
		{[]string{"-C", "dir", "-h"}, true},
		{[]string{"--helpful"}, false},
		{[]string{"-help"}, false},
	}
	for _, c := range cases {
		if got := wantsHelp(c.args); got != c.want {
			t.Errorf("wantsHelp(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

// dispatchCommands mirrors the command names handled by the switch in main(). Aliases
// (-h/--help/-v/--version) and the bare help/version verbs are excluded — they need no
// per-command help. Keep this in sync with main()'s dispatch.
var dispatchCommands = []string{
	"init", "task", "project", "run", "loop", "tick", "serve", "status",
	"answer", "digest", "skills", "mode", "feedback", "register", "login", "subscribe",
	"billing", "account", "link", "worker",
}

func TestEveryDispatchedCommandHasHelp(t *testing.T) {
	for _, cmd := range dispatchCommands {
		h, ok := commandHelp[cmd]
		if !ok {
			t.Errorf("command %q has no commandHelp entry", cmd)
			continue
		}
		if strings.TrimSpace(h) == "" {
			t.Errorf("command %q has empty help text", cmd)
		}
		// Focused help should name the command up front so it reads as that
		// command's usage, not the global dump.
		if !strings.Contains(h, "mago "+cmd) {
			t.Errorf("command %q help does not mention %q:\n%s", cmd, "mago "+cmd, h)
		}
	}
}

func TestNoStrayHelpEntries(t *testing.T) {
	known := make(map[string]bool, len(dispatchCommands))
	for _, c := range dispatchCommands {
		known[c] = true
	}
	for cmd := range commandHelp {
		if !known[cmd] {
			t.Errorf("commandHelp has entry %q with no matching dispatch command", cmd)
		}
	}
}
