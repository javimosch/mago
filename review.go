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

// reviewPR reviews a specific open PR (triggered by a pull_request webhook), with no standing
// review task. The MODEL never touches the repo — it judges the diff text (verdict + comment).
// Separately, when verification is enabled (MAGO_VERIFY/MAGO_VERIFY_CMD), the WORKER checks out the
// branch and runs build/tests; an auto-merge then requires both an approve AND a green check.
// No-op unless the PR's repo is one of this company's projects.
func (c *Company) reviewPR(prRepo string, prNum int) bool {
	// Accept PRs on the company repo itself (label-scoped mode C) or any registered project repo.
	known := prRepo == c.ghRepo
	for _, repo := range c.loadProjects() {
		if repo == prRepo {
			known = true
			break
		}
	}
	if !known {
		fmt.Fprintf(os.Stderr, "[review] PR #%d on %s is not a company repo/project — ignoring\n", prNum, prRepo)
		return false
	}
	reviewer := c.findReviewer()
	if reviewer == nil {
		fmt.Fprintf(os.Stderr, "[review] no reviewer agent (reviews: true) configured\n")
		return false
	}

	n := strconv.Itoa(prNum)
	diff, err := gh("-R", prRepo, "pr", "diff", n)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[review] diff PR #%d: %v\n", prNum, err)
		return false
	}
	if len(diff) > 12000 {
		diff = diff[:12000] + "\n...(diff truncated)"
	}
	fmt.Fprintf(os.Stderr, "[review] %s judging PR #%d in %s\n", reviewer.Name, prNum, prRepo)

	rubric := "Apply these EXACT merge criteria — do NOT invent any others:\n" +
		"APPROVE when ALL of these hold:\n" +
		"  1. The change does what the PR title/description says.\n" +
		"  2. It is syntactically/structurally valid (e.g. valid JSON, parseable code).\n" +
		"  3. It touches only the files it should — no stray or shared-file edits.\n" +
		"  4. No obvious correctness bug, security issue, destructive change, or committed secret.\n" +
		"REQUEST_CHANGES ONLY for a concrete BLOCKING defect from that list (a real bug, invalid syntax, " +
		"out-of-scope/destructive edit, or a leaked secret).\n" +
		"Do NOT request changes for missing tests, missing docs/README, comments, style, naming, or any " +
		"\"nice to have\" — those are NOT merge blockers. If the change is correct, scoped, and safe, APPROVE."

	// Intent gate: when the company has a declared direction, add a 5th blocker for work that violates
	// a no-touch constraint or the out-of-scope list. Kept narrow — we reject scope/direction
	// VIOLATIONS, not "doesn't perfectly match Now" — so the rubric stays permissive on quality nits.
	intent := ""
	if dir := c.directionContext(); dir != "" {
		rubric += "\n  5. It does NOT modify a no-touch area or do work listed as Out of scope below.\n" +
			"REQUEST_CHANGES if the PR clearly works on an Out-of-scope item or edits a declared no-touch area."
		intent = "\n\nCOMPANY DIRECTION (for criterion 5 — constraints & scope):\n" + dir
	}

	prompt := fmt.Sprintf("You are the code reviewer for pull request #%d on `%s`. Judge ONLY the diff "+
		"below, strictly against the criteria — nothing else.\n\n%s%s\n\nDIFF:\n```diff\n%s\n```\n\n"+
		"Respond with ONE fenced json code block and nothing else:\n"+
		"```json\n{\"verdict\": \"approve\" | \"request_changes\", \"comment\": \"1-2 sentences: which criteria it meets, or the specific blocking defect\"}\n```",
		prNum, prRepo, rubric, intent, diff)

	out, err := tauComplete(reviewer, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[review] model: %v\n", err)
		return false
	}
	verdict, comment := parseVerdict(out)
	if verdict == "" {
		fmt.Fprintf(os.Stderr, "[review] PR #%d: could not parse a verdict — not merging\n", prNum)
		return false
	}
	if strings.TrimSpace(comment) == "" {
		comment = "(no comment)"
	}
	// Verify the PR for real — check out the branch and run build/tests — so an auto-merge is
	// trustworthy, not just a diff the model liked. Runs only when the merge mode is "verified".
	vr := c.verifyPR(prRepo, prNum)
	merge := c.modeMerge() // live mode: review | verified | on
	mergeUnverified := os.Getenv("MAGO_MERGE_UNVERIFIED") == "1"

	approved := verdict == "approve"

	body := "**Review — " + reviewer.Title + ":** " + comment
	if vr.detail != "" {
		body += "\n\n**Verification:** " + vr.detail
	}

	suffix, doMerge, logmsg := decideMerge(approved, vr, merge, mergeUnverified)
	gh("-R", prRepo, "pr", "comment", n, "--body", body+suffix)
	if doMerge {
		if mout, merr := gh("-R", prRepo, "pr", "merge", n, "--squash", "--delete-branch"); merr != nil {
			fmt.Fprintf(os.Stderr, "[review] merge PR #%d failed: %v %s\n", prNum, merr, strings.TrimSpace(mout))
		} else {
			fmt.Fprintf(os.Stderr, "[review] PR #%d approved%s and merged\n", prNum, ifStr(vr.ok, " + verified", ""))
		}
	} else {
		fmt.Fprintf(os.Stderr, "[review] PR #%d: %s\n", prNum, logmsg)
	}
	return true
}

// decideMerge applies the merge-decision rules given a review verdict, verification result, and the
// company's live merge mode (review | verified | on). Split out from reviewPR so the branching can be
// unit-tested without a real GitHub PR or model call.
func decideMerge(approved bool, vr verifyResult, merge string, mergeUnverified bool) (suffix string, doMerge bool, logmsg string) {
	verifyFailed := vr.ran && !vr.ok
	switch {
	case verifyFailed: // build/tests failed — a real blocker the diff-only review can't see
		suffix = "\n\n_Changes requested: automated verification failed._"
		logmsg = "verification FAILED — changes requested"
	case !approved:
		logmsg = "changes requested (not merged)"
	case merge == "review":
		suffix = "\n\n_(approved" + ifStr(vr.ok, " + verified", "") + " — review-only mode; merge when ready.)_"
		logmsg = "approved — left for human merge (review mode)"
	case merge == "verified" && !vr.ok && !(!vr.ran && mergeUnverified):
		// verified mode but nothing green to stand on (no check ran, and not opted into merge-unverified).
		suffix = "\n\n_(approved but not auto-merged — no passing verification; set MAGO_MERGE_UNVERIFIED=1 or merge manually.)_"
		logmsg = "approved, unverified — not merged"
	default: // merge==on (LLM-approve), or merge==verified with a green check
		doMerge = true
	}
	return suffix, doMerge, logmsg
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
