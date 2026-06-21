package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
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

// reviewPR reviews a specific open PR (triggered by a pull_request webhook), with no
// standing review task. To stay reliable on huge repos, the model NEVER touches the repo
// or any tools: the worker fetches the diff, the model judges that text (verdict +
// comment), and the worker posts the comment and squash-merges on approval. No-op unless
// the PR's repo is one of this company's projects.
func (c *Company) reviewPR(prRepo string, prNum int) {
	inProject := false
	for _, repo := range c.loadProjects() {
		if repo == prRepo {
			inProject = true
			break
		}
	}
	if !inProject {
		fmt.Fprintf(os.Stderr, "[review] PR #%d on %s is not a company project — ignoring\n", prNum, prRepo)
		return
	}
	reviewer := c.findReviewer()
	if reviewer == nil {
		fmt.Fprintf(os.Stderr, "[review] no reviewer agent (reviews: true) configured\n")
		return
	}

	n := strconv.Itoa(prNum)
	diff, err := gh("-R", prRepo, "pr", "diff", n)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[review] diff PR #%d: %v\n", prNum, err)
		return
	}
	if len(diff) > 12000 {
		diff = diff[:12000] + "\n...(diff truncated)"
	}
	fmt.Fprintf(os.Stderr, "[review] %s judging PR #%d in %s\n", reviewer.Name, prNum, prRepo)

	prompt := fmt.Sprintf("You are %s reviewing a pull request. Judge ONLY the diff below — you have no "+
		"other context and need none.\n\n%s\n\nDIFF of PR #%d (%s):\n```diff\n%s\n```\n\n"+
		"Respond with ONE fenced json code block and nothing else:\n"+
		"```json\n{\"verdict\": \"approve\" | \"request_changes\", \"comment\": \"a 1-4 sentence assessment\"}\n```",
		reviewer.Title, strings.TrimSpace(reviewer.Persona), prNum, prRepo, diff)

	out, err := tauComplete(reviewer, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[review] model: %v\n", err)
		return
	}
	verdict, comment := parseVerdict(out)
	if verdict == "" {
		fmt.Fprintf(os.Stderr, "[review] PR #%d: could not parse a verdict — not merging\n", prNum)
		return
	}
	if strings.TrimSpace(comment) == "" {
		comment = "(no comment)"
	}
	gh("-R", prRepo, "pr", "comment", n, "--body", "**Review — "+reviewer.Title+":** "+comment)
	if verdict == "approve" {
		if mout, merr := gh("-R", prRepo, "pr", "merge", n, "--squash", "--delete-branch"); merr != nil {
			fmt.Fprintf(os.Stderr, "[review] merge PR #%d failed: %v %s\n", prNum, merr, strings.TrimSpace(mout))
		} else {
			fmt.Fprintf(os.Stderr, "[review] PR #%d approved and merged\n", prNum)
		}
	} else {
		fmt.Fprintf(os.Stderr, "[review] PR #%d: changes requested (not merged)\n", prNum)
	}
}

func parseVerdict(s string) (string, string) {
	for _, cand := range jsonCandidates(s) {
		var v struct {
			Verdict string `json:"verdict"`
			Comment string `json:"comment"`
		}
		if json.Unmarshal([]byte(cand), &v) == nil && v.Verdict != "" {
			return strings.ToLower(strings.TrimSpace(v.Verdict)), v.Comment
		}
	}
	return "", ""
}
