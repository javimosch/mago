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
	dir, _ := parseCompanyDir(args)
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

	tasks, err := comp.tasks.ListTasks()
	if err != nil {
		return false, err
	}
	for _, t := range tasks {
		// mago:go on a still-in-clarification task — promote it (drop planning state) so it
		// routes fresh to an implementer.
		if t.Go && (t.Clarify || t.Status == "needs_human") {
			comp.tasks.ClearClarify(t)
			fmt.Fprintf(os.Stderr, "[route] task #%s: mago:go -> promoting to implementation\n", t.ID)
		}
		if t.Status != "open" || t.Assignee != "" {
			continue
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

// routeTask asks the cheap model which agent should own a task, given the roster.
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
		roster.WriteString("- " + a.Name + ": " + a.Title + "\n")
	}
	prompt := fmt.Sprintf(`Assign this task to exactly one team member based on their role.

TASK TITLE: %s
TASK DETAIL: %s

TEAM (name: role):
%s
Reply with ONLY the name of the single best owner, nothing else.`,
		t.Title, oneLine(t.Body), roster.String())

	out, err := tauComplete(carrier, prompt)
	if err != nil {
		return ""
	}
	low := strings.ToLower(out)
	for _, a := range candidates {
		if strings.Contains(low, strings.ToLower(a.Name)) {
			return a.Name
		}
	}
	return ""
}
