package main

import (
	"fmt"
	"os"
)

// findReviewer returns the company's designated reviewer agent (reviews: true), or nil.
func (c *Company) findReviewer() *Agent {
	names, _ := c.loadAgentNames()
	for _, n := range names {
		a, err := c.loadAgent(n)
		if err != nil {
			continue
		}
		if a.Reviews {
			applyModelOverrides(a)
			return a
		}
	}
	return nil
}

// reviewPR runs the reviewer on a specific open PR (triggered by a pull_request webhook),
// with no standing review task: it comments its assessment and squash-merges if it passes.
// No-op unless the PR's repo is one of this company's projects.
func (c *Company) reviewPR(prRepo string, prNum int) {
	projName := ""
	for name, repo := range c.loadProjects() {
		if repo == prRepo {
			projName = name
			break
		}
	}
	if projName == "" {
		fmt.Fprintf(os.Stderr, "[review] PR #%d on %s is not a company project — ignoring\n", prNum, prRepo)
		return
	}
	reviewer := c.findReviewer()
	if reviewer == nil {
		fmt.Fprintf(os.Stderr, "[review] no reviewer agent (reviews: true) configured\n")
		return
	}
	ws := c.projectDir(projName)
	if err := ensureClone(ws, prRepo); err != nil {
		fmt.Fprintf(os.Stderr, "[review] clone %s: %v\n", prRepo, err)
		return
	}
	fmt.Fprintf(os.Stderr, "[review] %s reviewing PR #%d in %s\n", reviewer.Name, prNum, prRepo)

	prompt := fmt.Sprintf("# REVIEW TASK\n\nReview pull request #%d on the repo `%s`. You are the reviewer — "+
		"do NOT write or modify code, and do NOT browse the repository.\n"+
		"**Do NOT `ls`, `find`, `grep`, or read repo files — the repo may have thousands of files and "+
		"will stall you. `gh pr diff %d` shows you EXACTLY what changed, which is all you need to review.**\n"+
		"- Inspect the change with `gh pr diff %d` (just that one command).\n"+
		"- Post your assessment as a COMMENT: `gh pr comment %d --body \"...\"` (do NOT use `gh pr review`).\n"+
		"- If it meets the bar, merge it: `gh pr merge %d --squash --delete-branch`.\n"+
		"- If it does not, comment what must change and do NOT merge.\n\n%s",
		prNum, prRepo, prNum, prNum, prNum, prNum, reflectionInstruction)

	content, err := runTau(ws, reviewer, buildSystemPrompt(reviewer), prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[review] tau: %v\n", err)
		return
	}
	if refl, perr := parseReflection(content); perr == nil && refl != nil {
		fmt.Fprintf(os.Stderr, "[review] done: %s\n", oneLine(refl.Summary))
		c.writeJournal(reviewer, &Task{ID: fmt.Sprintf("pr-%d", prNum), Title: fmt.Sprintf("Review PR #%d", prNum)}, refl, nowStamp())
	} else {
		fmt.Fprintln(os.Stderr, "[review] (no parseable reflection; the gh PR ops may still have run)")
	}
}
