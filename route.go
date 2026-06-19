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
		if t.Status != "open" || t.Assignee != "" {
			continue
		}
		owner := routeTask(agents[0], t, agents)
		if owner == "" {
			fmt.Fprintf(os.Stderr, "[route] task #%s -> (no match)\n", t.ID)
			continue
		}
		if err := comp.tasks.Assign(t, owner); err != nil {
			fmt.Fprintf(os.Stderr, "[route] task #%s: %v\n", t.ID, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "[route] task #%s -> %s\n", t.ID, owner)
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

// routeTask asks the cheap model which agent should own a task, given the roster.
func routeTask(carrier *Agent, t *Task, agents []*Agent) string {
	var roster strings.Builder
	for _, a := range agents {
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
	for _, a := range agents {
		if strings.Contains(low, strings.ToLower(a.Name)) {
			return a.Name
		}
	}
	return ""
}
