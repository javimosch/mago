package main

import (
	"fmt"
	"time"
)

// cmdActivity prints onboarding observability: an account breakdown plus a timeline of recent
// signups, subscriptions, worker connects/disconnects, and repo links — so the operator can see
// leads arriving and whether their workers actually came online. Reads the same DB as the daemon
// (SQLite WAL allows the concurrent read), so it works while the platform is running.
//
//	mago-platform activity [N] [--limit N]
func cmdActivity(args []string) error {
	limit := 20
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--limit", "-n":
			if i+1 < len(args) {
				limit = int(atoi(args[i+1]))
				i++
			}
		default:
			if n := atoi(args[i]); n > 0 {
				limit = int(n)
			}
		}
	}

	st, err := openStore(expand(env("DB_PATH", "~/.mago-platform/platform.db")))
	if err != nil {
		return err
	}
	defer st.Close()

	s := st.Stats()
	fmt.Printf("accounts: %d total — %d active, %d trial-live, %d trial-expired, %d free\n",
		s.Total, s.Active, s.TrialLive, s.TrialExpired, s.Free)

	evs := st.RecentEvents(limit)
	if len(evs) == 0 {
		fmt.Println("\n(no activity recorded yet)")
		return nil
	}
	fmt.Printf("\nrecent activity (newest first, last %d):\n", len(evs))
	for _, e := range evs {
		who := e.Email
		if who == "" {
			who = fmt.Sprintf("user %d", e.UserID)
		}
		line := fmt.Sprintf("  %s  %-18s %s", time.Unix(e.TS, 0).Format("2006-01-02 15:04"), eventIcon(e.Kind), who)
		if e.Detail != "" {
			line += "  " + e.Detail
		}
		fmt.Println(line)
	}
	return nil
}

// eventIcon prefixes the event kind with a glyph so the timeline scans quickly.
func eventIcon(kind string) string {
	g := map[string]string{
		"signup":            "🆕 signup",
		"subscribed":        "💳 subscribed",
		"canceled":          "✖ canceled",
		"worker_connect":    "🟢 worker_connect",
		"worker_disconnect": "⚪ worker_disconnect",
		"linked":            "🔗 linked",
	}
	if v, ok := g[kind]; ok {
		return v
	}
	return kind
}
