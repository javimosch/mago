package main

import (
	"fmt"
	"os"
	"strings"
)

func (c *Company) loadAgentNames() ([]string, error) {
	entries, err := os.ReadDir(c.agentsDir())
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	return names, nil
}

// cmdTick reconciles the whole company once.
func cmdTick(args []string) error {
	dir, _, err := parseCompanyDir(args)
	if err != nil {
		return err
	}
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	_, err = reconcileOnce(comp)
	return err
}

// reconcileOnce routes open tasks to best-fit agents by role, then runs each agent
// on its actionable task. Returns whether any agent did work. No hand-ordering.
func reconcileOnce(comp *Company) (bool, error) {
	names, err := comp.loadAgentNames()
	if err != nil || len(names) == 0 {
		return false, fmt.Errorf("no agents in %s", comp.agentsDir())
	}
	var agents []*Agent
	for _, n := range names {
		a, err := comp.loadAgent(n)
		if err != nil {
			continue
		}
		applyModelOverrides(a)
		agents = append(agents, a)
	}
	// names came from the directory listing, so a non-empty roster can still yield zero
	// loadable agents (e.g. every file has invalid frontmatter) — routing would index
	// agents[0] below and panic. Fail clearly instead.
	if len(agents) == 0 {
		return false, fmt.Errorf("no loadable agents in %s", comp.agentsDir())
	}

	tasks, err := comp.tasks.ListTasks()
	if err != nil {
		return false, err
	}
	prCap := comp.modePRCap()  // open-PR backpressure per repo (0 = off)
	atCap := map[string]bool{} // memoize the gh count per repo within this reconcile
	knownAgents := map[string]bool{}
	for _, a := range agents {
		knownAgents[a.Name] = true
	}
	for _, t := range tasks {
		// mago:go on a still-in-clarification task — promote it (drop planning state) so it
		// routes fresh to an implementer.
		if t.Go && (t.Clarify || t.Status == "needs_human") {
			comp.tasks.ClearClarify(t)
			fmt.Fprintf(os.Stderr, "[route] task #%s: mago:go -> promoting to implementation\n", t.ID)
		}
		// A task pre-labeled with an agent that doesn't exist in the roster can't be picked up.
		// Bounce it so the router can reassign it to a real agent and the worker will pick it up.
		if t.Assignee != "" && !knownAgents[t.Assignee] {
			fmt.Fprintf(os.Stderr, "[route] task #%s: assignee %q not in roster — bouncing for re-routing\n", t.ID, t.Assignee)
			if err := comp.tasks.Bounce(t); err != nil {
				fmt.Fprintf(os.Stderr, "[route] task #%s: bounce failed: %v\n", t.ID, err)
				continue
			}
		}
		if t.Status != "open" || t.Assignee != "" {
			continue
		}
		// PR backpressure: don't start new work on a repo that already has its cap of open mago PRs —
		// leave the task open (unassigned) until reviews/merges drain it. Caps mago's cadence so a
		// repo on auto never piles past N open mago PRs (human PRs don't count).
		if prCap > 0 {
			repo := comp.taskRepo(t)
			if repo != "" {
				full, seen := atCap[repo]
				if !seen {
					full = repoOpenMagoPRs(repo) >= prCap
					atCap[repo] = full
				}
				if full {
					fmt.Fprintf(os.Stderr, "[route] task #%s: %s at PR cap (%d) — holding\n", t.ID, repo, prCap)
					continue
				}
			}
		}
		owner := ""
		if t.Clarify && !t.Go { // clarification phase -> the planner (head-of-product), not the implement router
			owner = plannerName(agents)
		}
		if owner == "" {
			owner = routeTask(agents[0], t, agents)
		}
		if owner == "" {
			fmt.Fprintf(os.Stderr, "[route] task #%s -> (no match)\n", t.ID)
			continue
		}
		if err := comp.tasks.Assign(t, owner); err != nil {
			fmt.Fprintf(os.Stderr, "[route] task #%s: %v\n", t.ID, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "[route] task #%s -> %s%s\n", t.ID, owner, ifStr(t.Clarify && !t.Go, " (clarify)", ""))
	}

	worked := false
	for _, a := range agents {
		res, err := runTick(comp, a.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[tick] %s: %v\n", a.Name, err)
			continue
		}
		if res.worked {
			worked = true
		} else {
			fmt.Fprintf(os.Stderr, "[tick] %s: nothing to do\n", a.Name)
		}
	}
	return worked, nil
}

// plannerName returns the designated planner (frontmatter `plans: true`) for the clarification
// phase, or "" if none — callers then fall back to the normal router.
func plannerName(agents []*Agent) string {
	for _, a := range agents {
		if a.Plans {
			return a.Name
		}
	}
	return ""
}

// implementerName returns the designated implementer (frontmatter `implements: true`), or "".
func implementerName(agents []*Agent) string {
	for _, a := range agents {
		if a.Implements {
			return a.Name
		}
	}
	return ""
}

// routeTask asks the cheap model which agent should own a task, given the roster, with explicit
// role rules and per-agent role hints so engineering work lands on the implementer (not the CMO).
func routeTask(carrier *Agent, t *Task, agents []*Agent) string {
	// Reviewers never own issue-tasks — they review PRs via the pull_request event path
	// (reviewPR), not task routing. Excluding them here keeps an implement task whose text
	// merely mentions "PR"/"review"/"merge" from being misrouted to a reviewer (and burning a
	// tick on a guaranteed reassign). Fall back to the full roster only if every agent reviews.
	var candidates []*Agent
	for _, a := range agents {
		if !isReviewerRole(a) {
			candidates = append(candidates, a)
		}
	}
	if len(candidates) == 0 {
		candidates = agents
	}
	var roster strings.Builder
	for _, a := range candidates {
		hint := " — marketing, READMEs, docs, copy, announcements"
		switch {
		case a.Implements:
			hint = " — code: features, bug fixes, refactors, tests, technical implementation"
		case a.Plans:
			hint = " — product specs, requirements, scoping, prioritization"
		}
		roster.WriteString("- " + a.Name + ": " + a.Title + hint + "\n")
	}
	prompt := fmt.Sprintf(`Assign this task to exactly one team member based on their role.

Routing rules:
- Anything that changes code (features, bug fixes, refactors, tests, technical work) -> the implementer.
- Marketing, READMEs, docs, copy, release notes, announcements -> the marketing role.
- Product specs, requirements, scoping, prioritization -> the product role.

TASK TITLE: %s
TASK DETAIL: %s

TEAM (name: role):
%s
Reply with ONLY the name of the single best owner, nothing else.`,
		t.Title, oneLine(t.Body), roster.String())

	if out, err := tauComplete(carrier, prompt); err == nil {
		low := strings.ToLower(out)
		for _, a := range candidates {
			if strings.Contains(low, strings.ToLower(a.Name)) {
				return a.Name
			}
		}
	}
	// Model failed or returned no recognizable name: default to the implementer (most tasks are
	// engineering work), then any candidate — never leave a routable task unowned.
	if impl := implementerName(candidates); impl != "" {
		return impl
	}
	if len(candidates) > 0 {
		return candidates[0].Name
	}
	return ""
}
