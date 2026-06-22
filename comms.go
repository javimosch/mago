package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// comms.go is the "beyond code" loop: a company event (a merged feature PR) triggers a
// NON-engineering deliverable. When a mago/task-* PR merges (opt-in MAGO_COMMS=1), the CMO drafts
// a user-facing release note and posts it on the PR — proving the company does more than ship code.
// It's terminal (a mago comment, not a new PR), so it can't loop.

// marketingAgent returns the CMO-style agent: the one that neither implements, plans, nor reviews
// (i.e. owns marketing/comms). Returns nil if the roster has no such role.
func (c *Company) marketingAgent() *Agent {
	names, err := c.loadAgentNames()
	if err != nil {
		return nil
	}
	for _, n := range names {
		a, err := c.loadAgent(n)
		if err != nil {
			continue
		}
		if !a.Implements && !a.Plans && !a.Reviews {
			applyModelOverrides(a)
			return a
		}
	}
	return nil
}

// shipReleaseNote asks the CMO to write a short user-facing release note for a just-merged PR and
// posts it as a comment on that PR. Returns whether it posted (false = no work, no budget charge).
func (c *Company) shipReleaseNote(repo string, prNum int, prTitle string) bool {
	cmo := c.marketingAgent()
	if cmo == nil {
		fmt.Fprintln(os.Stderr, "[comms] no marketing agent in roster — skipping release note")
		return false
	}
	prompt := fmt.Sprintf(`You are the CMO. A pull request just shipped (merged) in our product.
Write a short, upbeat, user-facing RELEASE NOTE announcing it: 2-4 sentences, plain language,
no code/jargon, no headers — just the announcement text. Reply with ONLY the note.

Shipped: PR #%d — %s`, prNum, oneLine(prTitle))

	note, err := tauComplete(cmo, prompt)
	if err != nil || strings.TrimSpace(note) == "" {
		fmt.Fprintf(os.Stderr, "[comms] CMO draft failed for PR #%d: %v\n", prNum, err)
		return false
	}
	// Lead with "**" so isMagoComment recognizes it (no self-wake on the resulting comment event).
	body := "**📣 Release note — " + cmo.Title + "** _(mago agent)_\n\n" + strings.TrimSpace(note)
	if _, err := gh("-R", repo, "pr", "comment", strconv.Itoa(prNum), "--body", body); err != nil {
		fmt.Fprintf(os.Stderr, "[comms] post release note on PR #%d: %v\n", prNum, err)
		return false
	}
	fmt.Fprintf(os.Stderr, "[comms] release note posted on PR #%d in %s (by %s)\n", prNum, repo, cmo.Name)
	return true
}
