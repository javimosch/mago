package main

import (
	"fmt"
	"os"
)

type tickResult struct {
	worked bool
	signal string
	status string
}

// cmdRun executes one tick for one agent.
func cmdRun(args []string) error {
	dir, rest := parseCompanyDir(args)
	if len(rest) < 1 {
		return fmt.Errorf("usage: mago run <agent> [-C dir]")
	}
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	res, err := runTick(comp, rest[0])
	if err != nil {
		return err
	}
	if !res.worked {
		fmt.Println("no actionable tasks — nothing to do this tick")
	}
	return nil
}

// runTick: pick the active task, claim it, drive tau, reflect, write back.
func runTick(comp *Company, agentName string) (tickResult, error) {
	a, err := comp.loadAgent(agentName)
	if err != nil {
		return tickResult{}, err
	}
	applyModelOverrides(a)
	task, err := comp.tasks.PickActiveTask(agentName)
	if err != nil {
		return tickResult{}, err
	}
	if task == nil {
		return tickResult{worked: false, signal: "idle"}, nil
	}
	if err := comp.tasks.Claim(task, agentName); err != nil {
		return tickResult{}, err
	}
	fmt.Fprintf(os.Stderr, "=== mago tick: %s -> task #%s %q  [%s/%s] ===\n",
		agentName, task.ID, task.Title, a.Provider, a.Model)
	content, err := runTau(comp.workspaceDir(), a, buildSystemPrompt(a), comp.buildBriefing(a, task))
	if err != nil {
		return tickResult{}, err
	}
	refl, err := parseReflection(content)
	if err != nil {
		comp.writeRawFailure(a, task, content)
		return tickResult{}, err
	}
	comp.writeBack(a, task, refl, content)
	comp.printRunResult(a, task, refl)
	return tickResult{worked: true, signal: refl.CadenceSignal, status: refl.TaskStatus}, nil
}

// applyModelOverrides lets the smoke test switch provider/model via env without
// editing agent files (e.g. MAGO_PROVIDER=opencode-go).
func applyModelOverrides(a *Agent) {
	if p := os.Getenv("MAGO_PROVIDER"); p != "" {
		a.Provider = p
	}
	if m := os.Getenv("MAGO_MODEL"); m != "" {
		a.Model = m
	}
}
