package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// verify.go gives the reviewer real teeth: instead of only judging a diff, it checks out the PR
// branch and runs the project's build/tests, so an auto-merge stands on a passing check rather than
// the model's opinion. Opt-in: MAGO_VERIFY=1 (auto-detect, e.g. Go) or MAGO_VERIFY_CMD="<cmd>".

type verifyResult struct {
	ran    bool   // a check actually ran
	ok     bool   // ...and passed
	detail string // human-readable line for the review comment ("" = verification disabled)
}

func verifyEnabled() bool {
	return os.Getenv("MAGO_VERIFY") == "1" || strings.TrimSpace(os.Getenv("MAGO_VERIFY_CMD")) != ""
}

// verifyCommand returns the shell command + label to verify the checkout, or "" if none applies.
func verifyCommand(dir string) (cmd, label string) {
	if v := strings.TrimSpace(os.Getenv("MAGO_VERIFY_CMD")); v != "" {
		return v, "MAGO_VERIFY_CMD"
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return "go build ./... && go test ./...", "go build + test"
	}
	return "", ""
}

// verifyPR checks out PR #prNum of repo and runs the verify command. Returns ran=false (no merge
// gate) when verification is disabled or no check could be determined.
func (c *Company) verifyPR(repo string, prNum int) verifyResult {
	if !verifyEnabled() {
		return verifyResult{}
	}
	dir := filepath.Join(c.magoDir(), "verify", sanitize(repo))
	if err := ensureClone(dir, repo); err != nil {
		return verifyResult{detail: "could not clone for verification"}
	}
	br := fmt.Sprintf("mago-verify-%d", prNum)
	if out, err := gitRun(dir, "fetch", "-q", "origin", fmt.Sprintf("pull/%d/head:%s", prNum, br)); err != nil {
		return verifyResult{detail: "could not fetch PR for verification: " + oneLine(out)}
	}
	gitRun(dir, "checkout", "-q", br)
	defer func() { // leave the clone on a clean default branch for next time
		gitRun(dir, "checkout", "-q", "-")
		gitRun(dir, "branch", "-q", "-D", br)
	}()

	cmd, label := verifyCommand(dir)
	if cmd == "" {
		return verifyResult{detail: "no automated checks detected (set MAGO_VERIFY_CMD)"}
	}
	fmt.Fprintf(os.Stderr, "[verify] PR #%d: %s\n", prNum, label)
	out, err := runShell(dir, cmd, 8*time.Minute)
	if err != nil {
		return verifyResult{ran: true, ok: false,
			detail: label + " **FAILED**:\n```\n" + lastLines(out, 15) + "\n```"}
	}
	return verifyResult{ran: true, ok: true, detail: label + " passed ✅"}
}

func runShell(dir, command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// lastLines returns the last n non-empty-trimmed lines of s (for a compact failure excerpt).
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
