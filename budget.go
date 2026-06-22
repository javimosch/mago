package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// budget.go is the autonomy guardrail: a per-day cap on how many autonomous work cycles the company
// runs, so a company can be left running for days without surprises. A "cycle" is one work-producing
// wake — a reconcile (route+implement), a PR review, a release note, or a proactive planning round.
// Set MAGO_DAILY_BUDGET=<n>; 0 (default) is unlimited. Usage persists in .mago/usage.json and resets
// at UTC midnight. When the cap is hit the worker pauses autonomous work (and says so in the digest).

type budgetUsage struct {
	Day       string `json:"day"`        // UTC date the counts belong to
	Actions   int    `json:"actions"`    // work cycles run today
	PausedDay string `json:"paused_day"` // date we last logged a budget pause (log once/day)
}

func dailyBudget() int { return int(atoiSafe(os.Getenv("MAGO_DAILY_BUDGET"))) } // 0 = unlimited

func utcDay() string { return time.Now().UTC().Format("2006-01-02") }

func (c *Company) usageFile() string { return filepath.Join(c.magoDir(), "usage.json") }

func (c *Company) loadUsage() budgetUsage {
	var u budgetUsage
	if b, err := os.ReadFile(c.usageFile()); err == nil {
		json.Unmarshal(b, &u)
	}
	if u.Day != utcDay() { // a new day resets the counters
		u = budgetUsage{Day: utcDay()}
	}
	return u
}

func (c *Company) saveUsage(u budgetUsage) {
	if b, err := json.MarshalIndent(u, "", "  "); err == nil {
		os.WriteFile(c.usageFile(), b, 0o644)
	}
}

// overBudget reports whether today's autonomous-work cap is exhausted.
func (c *Company) overBudget() bool {
	cap := dailyBudget()
	return cap > 0 && c.loadUsage().Actions >= cap
}

// recordAction counts one autonomous work cycle against today's budget.
func (c *Company) recordAction() {
	if dailyBudget() <= 0 {
		return
	}
	u := c.loadUsage()
	u.Actions++
	c.saveUsage(u)
}

// actionsToday returns the number of work cycles run today (for the digest).
func (c *Company) actionsToday() int { return c.loadUsage().Actions }

// guardBudget reports whether work should be skipped because the daily cap is hit, logging the
// pause once per UTC day so it doesn't spam on every heartbeat.
func (c *Company) guardBudget(what string) bool {
	if !c.overBudget() {
		return false
	}
	if u := c.loadUsage(); u.PausedDay != utcDay() {
		fmt.Fprintf(os.Stderr, "[budget] daily cap (%d) reached — pausing autonomous work until UTC midnight (skipped %s)\n", dailyBudget(), what)
		u.PausedDay = utcDay()
		c.saveUsage(u)
	}
	return true
}
