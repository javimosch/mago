package main

import (
	"fmt"
	"os"
)

// cmdRun executes one tick: brief -> drive tau -> reflect -> write back.
func cmdRun(args []string) error {
	dir, rest := parseCompanyDir(args)
	if len(rest) < 1 {
		return fmt.Errorf("usage: mago run <agent> [-C dir]")
	}
	agentName := rest[0]
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	a, err := comp.loadAgent(agentName)
	if err != nil {
		return err
	}
	applyModelOverrides(a)

	task, err := comp.pickActiveTask(agentName)
	if err != nil {
		return err
	}
	if task == nil {
		fmt.Println("no actionable tasks — nothing to do this tick")
		return nil
	}
	if err := comp.claim(task, agentName); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "=== mago tick: %s -> task #%s %q  [%s/%s] ===\n",
		agentName, task.ID, task.Title, a.Provider, a.Model)

	content, err := runTau(comp.workspaceDir(), a, buildSystemPrompt(a), comp.buildBriefing(a, task))
	if err != nil {
		return err
	}
	refl, err := parseReflection(content)
	if err != nil {
		comp.writeRawFailure(a, task, content)
		return err
	}
	if err := comp.writeBack(a, task, refl, content); err != nil {
		return err
	}
	comp.printRunResult(a, task, refl)
	return nil
}

// applyModelOverrides lets the smoke test switch provider/model via env without
// editing agent files (e.g. MAGO_PROVIDER=xiaomi).
func applyModelOverrides(a *Agent) {
	if p := os.Getenv("MAGO_PROVIDER"); p != "" {
		a.Provider = p
	}
	if m := os.Getenv("MAGO_MODEL"); m != "" {
		a.Model = m
	}
}
