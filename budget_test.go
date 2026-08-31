package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBudget(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "t"}

	// No cap: never over budget, recordAction is a no-op.
	os.Unsetenv("MAGO_DAILY_BUDGET")
	c.recordAction()
	if c.overBudget() {
		t.Fatal("no cap should never be over budget")
	}

	// Cap of 2: over once two cycles are recorded.
	t.Setenv("MAGO_DAILY_BUDGET", "2")
	c.saveUsage(budgetUsage{Day: utcDay()})
	if c.overBudget() {
		t.Fatal("0/2 should not be over budget")
	}
	c.recordAction()
	if c.overBudget() {
		t.Fatal("1/2 should not be over budget")
	}
	c.recordAction()
	if !c.overBudget() {
		t.Fatal("2/2 should be over budget")
	}
	if c.actionsToday() != 2 {
		t.Fatalf("actionsToday = %d, want 2", c.actionsToday())
	}
	if !c.guardBudget("x") {
		t.Fatal("guardBudget should report paused when over budget")
	}
	// Calling guardBudget again on the same UTC day should still pause
	// but must not re-emit the log line.
	if !c.guardBudget("x") {
		t.Fatal("guardBudget should keep reporting paused on the same day")
	}

	// A stale day resets the counters.
	c.saveUsage(budgetUsage{Day: "2000-01-01", Actions: 99})
	if c.actionsToday() != 0 {
		t.Fatalf("stale day should reset, got %d", c.actionsToday())
	}
	if c.overBudget() {
		t.Fatal("stale day should not be over budget")
	}
}

// TestBudget_TrimmedCap verifies that whitespace around MAGO_DAILY_BUDGET does not
// prevent the cap from being applied, matching the TrimSpace invariant used elsewhere.
func TestBudget_TrimmedCap(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "t"}

	t.Setenv("MAGO_DAILY_BUDGET", "  2  ")
	c.saveUsage(budgetUsage{Day: utcDay()})
	c.recordAction()
	c.recordAction()
	if !c.overBudget() {
		t.Fatal("whitespace-padded cap of 2 should be over budget after 2 actions")
	}
}

func TestBudget_InvalidCap(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "t"}

	cases := []struct {
		label string
		value string
	}{
		{"empty", ""},
		{"invalid", "abc"},
		{"negative", "-5"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Setenv("MAGO_DAILY_BUDGET", tc.value)
			if c.overBudget() {
				t.Fatalf("%q cap should not be over budget", tc.value)
			}
			c.recordAction()
			if c.overBudget() {
				t.Fatalf("%q cap should remain unlimited after recordAction", tc.value)
			}
			if _, err := os.Stat(c.usageFile()); err == nil {
				t.Fatalf("%q cap should not write usage.json", tc.value)
			}
		})
	}
}
