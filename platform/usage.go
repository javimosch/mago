package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// usage.go surfaces relay-usage aggregation — adoption depth derived from the GitHub events mago
// actually acts on (issues filed, PRs flowing, comments), per account. `mago-platform usage` is the
// operator's "who is really using mago" view; GET /api/usage is the per-account feed an operator
// agent (the planner) can read to sense real demand instead of only its own judgment.

// cmdUsage prints per-account relay activity over the last N days, most-active first.
//
//	mago-platform usage [days]   (default 7)
func cmdUsage(args []string) error {
	days := 7
	for _, a := range args {
		if n := atoi(a); n > 0 {
			days = int(n)
		}
	}
	st, err := openStore(expand(env("DB_PATH", "~/.mago-platform/platform.db")))
	if err != nil {
		return err
	}
	defer st.Close()

	rows := st.UsageByAccount(days)
	if len(rows) == 0 {
		fmt.Printf("no relayed GitHub activity in the last %dd — no usage to report yet.\n", days)
		fmt.Println("(usage accrues as workers act on real repos; instrumentation captures events from now on.)")
		return nil
	}
	fmt.Printf("relay usage — last %dd · adoption depth from the GitHub events mago acted on\n\n", days)
	for _, u := range rows {
		who := u.Email
		if who == "" {
			who = fmt.Sprintf("account %d", u.AccountID)
		}
		fmt.Printf("%-34s %-7s  %3d events  (issues %d · PRs %d · comments %d) · %d repo(s) · last %s\n",
			who, orStr(u.Plan, "?"), u.Total, u.Issues, u.PRs, u.Comments, len(u.Repos), agoStr(u.LastTs))
		if len(u.Repos) > 0 {
			fmt.Printf("    %s\n", strings.Join(u.Repos, ", "))
		}
	}
	return nil
}

// handleUsage returns the authenticated account's usage rollup (GET /api/usage?days=7). This is the
// real outcome signal an operator agent's planner can fold into its sense() — non-zero only when a
// real operator is running mago against real repos.
func (s *server) handleUsage(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.authUID(r)
	if !ok {
		httpErr(w, 401, "unauthorized")
		return
	}
	days := 7
	if d := atoi(r.URL.Query().Get("days")); d > 0 {
		days = int(d)
	}
	writeJSON(w, 200, s.store.UsageForAccount(uid, days))
}

// agoStr renders a unix ts as a compact "5m / 3h / 2d ago", or "never" for 0.
func agoStr(ts int64) string {
	if ts == 0 {
		return "never"
	}
	d := time.Since(time.Unix(ts, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours())/24)
	}
}
