package main

import (
	"fmt"
	"strings"
	"time"
)

// digest.go is the "what your company did" summary — pull-based (run it, or cron it) so the CEO can
// check in on an unattended company at a glance: mission, backlog by status, recent PR throughput,
// anything waiting on the human (HITL), and today's autonomy-budget usage.
//
//	mago digest [-C dir]
func cmdDigest(args []string) error {
	dir, _, err := parseCompanyDir(args)
	if err != nil {
		return err
	}
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}

	fmt.Printf("# %s — daily digest (%s UTC)\n\n", comp.Name, utcDay())

	mission := comp.missionText()
	if mission == "" {
		fmt.Println("Mission: (unset — set it in STATE.md ## Mission to enable proactive planning)")
	} else {
		fmt.Printf("Mission: %s\n", oneLine(firstLine(mission)))
	}

	// Backlog by status (from the task backend).
	tasks, terr := comp.tasks.ListTasks()
	if terr != nil {
		fmt.Printf("\nBacklog: (could not list tasks: %v)\n", terr)
	} else {
		n := map[string]int{}
		for _, t := range tasks {
			n[t.Status]++
		}
		where := "local"
		if comp.ghRepo != "" {
			where = comp.ghRepo
		}
		fmt.Printf("\nBacklog (%s):\n  open %d · in-progress %d · blocked %d · needs-you %d · done %d\n",
			where, n["open"], n["in_progress"], n["blocked"], n["needs_human"], n["done"])
	}

	// PR throughput on the company repo, incl. the autonomy metric: PRs mago shipped (mago/* branches).
	if comp.ghRepo != "" {
		since := time.Now().AddDate(0, 0, -1).UTC().Format("2006-01-02")
		merged := ghCount(comp.ghRepo, "pr", "list", "--state", "merged", "--search", "merged:>="+since, "--json", "number", "--jq", "length")
		open := ghCount(comp.ghRepo, "pr", "list", "--state", "open", "--json", "number", "--jq", "length")
		magoShipped := ghCount(comp.ghRepo, "pr", "list", "--state", "merged", "--search", "merged:>="+since+" head:mago/", "--json", "number", "--jq", "length")
		fmt.Printf("\nPull requests (last 24h):\n  merged %s · open %s · shipped by mago %s\n", numOr(merged), numOr(open), numOr(magoShipped))
	}

	// Anything waiting on the human.
	if hitl, _ := comp.tasks.PendingHITL(); len(hitl) > 0 {
		fmt.Printf("\nNeeds you (HITL):\n")
		for _, h := range hitl {
			fmt.Printf("  - %s\n", oneLine(h))
		}
	} else {
		fmt.Printf("\nNeeds you (HITL): none\n")
	}

	// Autonomy budget.
	fmt.Printf("\nAutonomy today: ")
	if cap := dailyBudget(); cap > 0 {
		fmt.Printf("%d / %d work cycles", comp.actionsToday(), cap)
		if comp.overBudget() {
			fmt.Printf("  ⏸ paused (daily cap reached; resumes UTC midnight)")
		}
		fmt.Println()
	} else {
		fmt.Printf("%d work cycles (no MAGO_DAILY_BUDGET cap set)\n", comp.actionsToday())
	}
	return nil
}

// ghCount runs a gh query whose --jq yields a count and returns it, or -1 if the query failed.
func ghCount(repo string, args ...string) int {
	out, err := gh(append([]string{"-R", repo}, args...)...)
	if err != nil {
		return -1
	}
	return int(atoiSafe(strings.TrimSpace(out)))
}

func numOr(n int) string {
	if n < 0 {
		return "?"
	}
	return fmt.Sprintf("%d", n)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
