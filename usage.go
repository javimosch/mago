package main

import (
	"fmt"
	"strings"
)

// usage.go is the planner's demand sensor: the worker fetches this account's recent relay activity
// (GET /api/usage — the GitHub events mago acted on, per account) and folds it into the planning
// prompt. So the planner can weigh REAL operator demand (issues filed, questions, PRs flowing on the
// repos it serves) against the static roadmap — the outcome loop reading the world, not just judgment.
// Best-effort: silent when not logged in, the platform is unreachable, or there's no activity yet.

type accountUsage struct {
	Repos    []string `json:"repos"`
	Total    int      `json:"total"`
	Issues   int      `json:"issues"`
	PRs      int      `json:"prs"`
	Comments int      `json:"comments"`
	LastTs   int64    `json:"last_ts"`
}

// renderUsageSignal formats an account's usage as a one-line planner signal, scoped to the repos this
// company serves (so the planner sees demand on ITS surface). Returns "" when there's no activity.
func renderUsageSignal(u accountUsage, myRepos []string) string {
	if u.Total == 0 {
		return ""
	}
	mine := map[string]bool{}
	for _, r := range myRepos {
		mine[r] = true
	}
	var active []string
	for _, r := range u.Repos {
		if mine[r] {
			active = append(active, r)
		}
	}
	return fmt.Sprintf("last 7d on this account: %d issue · %d PR · %d comment events; active repos you serve: %s",
		u.Issues, u.PRs, u.Comments, orNone(strings.Join(active, ", ")))
}

// usageContext fetches /api/usage and renders the planner signal, or "" on any error / no activity.
func (c *Company) usageContext() string {
	cfg := loadConfig()
	if cfg.Token == "" {
		return "" // not logged in -> no signal to read
	}
	var u accountUsage
	if err := cfg.platformDo("GET", "/api/usage?days=7", nil, true, &u); err != nil {
		return ""
	}
	return renderUsageSignal(u, c.repos())
}
