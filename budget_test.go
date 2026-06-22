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

	// A stale day resets the counters.
	c.saveUsage(budgetUsage{Day: "2000-01-01", Actions: 99})
	if c.actionsToday() != 0 {
		t.Fatalf("stale day should reset, got %d", c.actionsToday())
	}
	if c.overBudget() {
		t.Fatal("stale day should not be over budget")
	}
}
