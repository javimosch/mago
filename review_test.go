package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseVerdict(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantVerdict string
		wantComment string
	}{
		{
			name:        "fenced json approve",
			in:          "Looks fine.\n```json\n{\"verdict\": \"approve\", \"comment\": \"meets all criteria\"}\n```",
			wantVerdict: "approve",
			wantComment: "meets all criteria",
		},
		{
			name:        "fenced json request_changes",
			in:          "```json\n{\"verdict\": \"REQUEST_CHANGES\", \"comment\": \"leaks a secret\"}\n```",
			wantVerdict: "request_changes",
			wantComment: "leaks a secret",
		},
		{
			name:        "bare json no fences",
			in:          "{\"verdict\": \"approve\", \"comment\": \"ok\"}",
			wantVerdict: "approve",
			wantComment: "ok",
		},
		{
			name:        "json embedded in prose",
			in:          "Here is my verdict: {\"verdict\": \"approve\", \"comment\": \"good\"} thanks",
			wantVerdict: "approve",
			wantComment: "good",
		},
		{
			name:        "whitespace/case normalized",
			in:          "```json\n{\"verdict\": \"  Approve \", \"comment\": \"ok\"}\n```",
			wantVerdict: "approve",
			wantComment: "ok",
		},
		{
			name:        "missing verdict field",
			in:          "```json\n{\"comment\": \"no verdict here\"}\n```",
			wantVerdict: "",
			wantComment: "",
		},
		{
			name:        "not json at all",
			in:          "I approve this PR.",
			wantVerdict: "",
			wantComment: "",
		},
		{
			name:        "empty string",
			in:          "",
			wantVerdict: "",
			wantComment: "",
		},
		{
			name:        "multiple fenced blocks uses last",
			in:          "```json\n{\"verdict\": \"request_changes\", \"comment\": \"draft\"}\n```\nthinking more...\n```json\n{\"verdict\": \"approve\", \"comment\": \"final\"}\n```",
			wantVerdict: "approve",
			wantComment: "final",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotVerdict, gotComment := parseVerdict(tc.in)
			if gotVerdict != tc.wantVerdict {
				t.Errorf("verdict = %q, want %q", gotVerdict, tc.wantVerdict)
			}
			if gotComment != tc.wantComment {
				t.Errorf("comment = %q, want %q", gotComment, tc.wantComment)
			}
		})
	}
}

func TestDecideMerge(t *testing.T) {
	cases := []struct {
		name            string
		approved        bool
		vr              verifyResult
		merge           string
		mergeUnverified bool
		wantDoMerge     bool
		wantLogContains string
	}{
		{
			name:            "verification failed blocks merge regardless of mode",
			approved:        true,
			vr:              verifyResult{ran: true, ok: false, detail: "go test **FAILED**"},
			merge:           "on",
			wantDoMerge:     false,
			wantLogContains: "verification FAILED",
		},
		{
			name:            "not approved never merges",
			approved:        false,
			vr:              verifyResult{},
			merge:           "on",
			wantDoMerge:     false,
			wantLogContains: "changes requested",
		},
		{
			name:            "review mode never auto-merges even when verified",
			approved:        true,
			vr:              verifyResult{ran: true, ok: true, detail: "passed"},
			merge:           "review",
			wantDoMerge:     false,
			wantLogContains: "review mode",
		},
		{
			name:            "verified mode with no check ran and not opted into unverified merge",
			approved:        true,
			vr:              verifyResult{ran: false, ok: false},
			merge:           "verified",
			mergeUnverified: false,
			wantDoMerge:     false,
			wantLogContains: "unverified",
		},
		{
			name:            "verified mode with no check ran but opted into unverified merge",
			approved:        true,
			vr:              verifyResult{ran: false, ok: false},
			merge:           "verified",
			mergeUnverified: true,
			wantDoMerge:     true,
			wantLogContains: "",
		},
		{
			name:            "verified mode with a green check merges",
			approved:        true,
			vr:              verifyResult{ran: true, ok: true, detail: "passed"},
			merge:           "verified",
			wantDoMerge:     true,
			wantLogContains: "",
		},
		{
			name:            "on mode merges on approve with no verification run",
			approved:        true,
			vr:              verifyResult{},
			merge:           "on",
			wantDoMerge:     true,
			wantLogContains: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			suffix, doMerge, logmsg := decideMerge(tc.approved, tc.vr, tc.merge, tc.mergeUnverified)
			if doMerge != tc.wantDoMerge {
				t.Errorf("doMerge = %v, want %v (suffix=%q logmsg=%q)", doMerge, tc.wantDoMerge, suffix, logmsg)
			}
			if tc.wantLogContains != "" && !strings.Contains(logmsg, tc.wantLogContains) {
				t.Errorf("logmsg = %q, want it to contain %q", logmsg, tc.wantLogContains)
			}
		})
	}
}

func TestMergeUnverifiedEnabled(t *testing.T) {
	t.Run("opted in", func(t *testing.T) {
		t.Setenv("MAGO_MERGE_UNVERIFIED", "1")
		if !mergeUnverifiedEnabled() {
			t.Error("mergeUnverifiedEnabled() = false, want true")
		}
	})

	t.Run("padded value is trimmed", func(t *testing.T) {
		t.Setenv("MAGO_MERGE_UNVERIFIED", "  1  ")
		if !mergeUnverifiedEnabled() {
			t.Error("mergeUnverifiedEnabled() = false, want true for padded value")
		}
	})

	t.Run("unset is false", func(t *testing.T) {
		t.Setenv("MAGO_MERGE_UNVERIFIED", "")
		if mergeUnverifiedEnabled() {
			t.Error("mergeUnverifiedEnabled() = true, want false")
		}
	})

	t.Run("whitespace-only is false", func(t *testing.T) {
		t.Setenv("MAGO_MERGE_UNVERIFIED", "   ")
		if mergeUnverifiedEnabled() {
			t.Error("mergeUnverifiedEnabled() = true, want false for whitespace-only value")
		}
	})

	t.Run("other value is false", func(t *testing.T) {
		t.Setenv("MAGO_MERGE_UNVERIFIED", "yes")
		if mergeUnverifiedEnabled() {
			t.Error("mergeUnverifiedEnabled() = true, want false")
		}
	})
}

// --- findReviewer ---

func TestFindReviewer_NoAgents(t *testing.T) {
	c := newTestCompany(t)
	if got := c.findReviewer(); got != nil {
		t.Errorf("findReviewer() = %v, want nil for a company with no agent files", got)
	}
}

func TestFindReviewer_NoneMarkedReviewer(t *testing.T) {
	c := newTestCompany(t)
	writeAgentFile(t, c, "cto", "---\ntitle: CTO\nimplements: true\n---\n")
	writeAgentFile(t, c, "hop", "---\ntitle: Head of Product\nplans: true\n---\n")
	if got := c.findReviewer(); got != nil {
		t.Errorf("findReviewer() = %v, want nil when no agent has reviews: true", got)
	}
}

func TestFindReviewer_SelectsReviewer(t *testing.T) {
	c := newTestCompany(t)
	writeAgentFile(t, c, "cto", "---\ntitle: CTO\nimplements: true\n---\n")
	writeAgentFile(t, c, "hoe", "---\ntitle: Head of Org Engineering\nreviews: true\n---\n")
	got := c.findReviewer()
	if got == nil {
		t.Fatal("findReviewer() = nil, want the agent with reviews: true")
	}
	if got.Name != "hoe" {
		t.Errorf("findReviewer().Name = %q, want %q", got.Name, "hoe")
	}
	if !got.Reviews {
		t.Error("findReviewer() returned an agent with Reviews == false")
	}
}

func TestFindReviewer_SkipsUnreadableAgentAndFindsNext(t *testing.T) {
	c := newTestCompany(t)
	// "bad" has an unknown frontmatter key, so loadAgent errors on it; findReviewer
	// must skip it (continue) rather than abort the scan.
	writeAgentFile(t, c, "bad", "---\ntitle: Broken\nnotarealkey: true\n---\n")
	writeAgentFile(t, c, "zzz-reviewer", "---\ntitle: Reviewer\nreviews: true\n---\n")
	got := c.findReviewer()
	if got == nil {
		t.Fatal("findReviewer() = nil, want the valid reviewer agent despite the broken sibling file")
	}
	if got.Name != "zzz-reviewer" {
		t.Errorf("findReviewer().Name = %q, want %q", got.Name, "zzz-reviewer")
	}
}

// --- reviewPR repo-scoping ---
//
// reviewPR checks repo-scoping (company repo or a registered project) before doing any
// gh/model I/O, so these cases exercise that branch directly without a real GitHub PR.

func TestReviewPR_UnknownRepoIgnored(t *testing.T) {
	c := newTestCompany(t)
	c.ghRepo = "acme/backlog"
	if ok := c.reviewPR("someone-else/unrelated", 1); ok {
		t.Error("reviewPR() = true, want false for a repo that is neither the company repo nor a project")
	}
}

func TestReviewPR_CompanyRepoAcceptedButNoReviewerConfigured(t *testing.T) {
	c := newTestCompany(t)
	c.ghRepo = "acme/backlog"
	// Known repo, but no reviewer agent exists — reviewPR should stop there (still no gh/model I/O).
	if ok := c.reviewPR("acme/backlog", 1); ok {
		t.Error("reviewPR() = true, want false when no reviewer agent (reviews: true) is configured")
	}
}

func TestReviewPR_ProjectRepoAccepted(t *testing.T) {
	c := newTestCompany(t)
	c.ghRepo = "acme/backlog"
	projectsJSON := `{"widget": "acme/widget"}`
	if err := os.WriteFile(c.projectsConfigFile(), []byte(projectsJSON), 0o644); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}
	// "acme/widget" is not the company repo but IS a registered project repo, so it's in scope.
	// No reviewer is configured, so reviewPR still returns false — but via the "no reviewer"
	// branch, proving the repo-scoping check itself passed.
	if ok := c.reviewPR("acme/widget", 7); ok {
		t.Error("reviewPR() = true, want false (no reviewer configured) but the repo should be in scope")
	}
}

// TestReviewPR_DiffError verifies that reviewPR returns false and stops before any model
// or verification work when the gh pr diff call fails (network/auth/missing PR).
func TestReviewPR_DiffError(t *testing.T) {
	c := newTestCompany(t)
	c.ghRepo = "acme/backlog"
	writeAgentFile(t, c, "reviewer", "---\nname: reviewer\ntitle: Reviewer\nreviews: true\n---\n")

	ghDir := t.TempDir()
	script := `#!/bin/sh
if [ "$3" = "pr" ] && [ "$4" = "diff" ]; then
	echo "diff fetch failed" >&2
fi
exit 1
`
	if err := os.WriteFile(filepath.Join(ghDir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", ghDir+":"+os.Getenv("PATH"))

	if got := c.reviewPR("acme/backlog", 1); got {
		t.Error("reviewPR() = true, want false when gh pr diff fails")
	}
}

// writeFakeReviewBins installs fake `gh` and `tau` binaries on PATH. The fake gh
// logs every invocation to $GH_LOG and prints diffBody for `pr diff`; the fake tau
// answers tauComplete with content. Returns the path of the gh log file.
func writeFakeReviewBins(t *testing.T, diffBody, tauContent string) string {
	t.Helper()
	dir := t.TempDir()
	ghLog := filepath.Join(dir, "gh.log")
	t.Setenv("GH_LOG", ghLog)

	ghScript := `#!/bin/sh
echo "$@" >> "$GH_LOG"
if [ "$3" = "pr" ] && [ "$4" = "diff" ]; then
	cat "$GH_DIFF"
fi
exit 0
`
	diffFile := filepath.Join(dir, "diff.txt")
	if err := os.WriteFile(diffFile, []byte(diffBody), 0o644); err != nil {
		t.Fatalf("write diff fixture: %v", err)
	}
	t.Setenv("GH_DIFF", diffFile)
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(ghScript), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}

	tauScript := `#!/bin/sh
printf '%s\n' "$TAU_OUT"
`
	t.Setenv("TAU_OUT", tauContent)
	if err := os.WriteFile(filepath.Join(dir, "tau"), []byte(tauScript), 0o755); err != nil {
		t.Fatalf("write fake tau: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return ghLog
}

// TestReviewPR_ApproveMerges exercises the full review path end-to-end with fake
// binaries: a reviewer agent + approve verdict in the default merge mode ("on")
// must post the review comment AND squash-merge the PR.
func TestReviewPR_ApproveMerges(t *testing.T) {
	// Deterministic merge mode: no env may flip it to review/verified or disable verify.
	t.Setenv("MAGO_NO_MERGE", "")
	t.Setenv("MAGO_VERIFY", "")
	t.Setenv("MAGO_VERIFY_CMD", "")
	t.Setenv("MAGO_MERGE_UNVERIFIED", "")
	t.Setenv("MAGO_PROVIDER", "")
	t.Setenv("MAGO_MODEL", "")

	c := newTestCompany(t)
	c.ghRepo = "acme/backlog"
	writeAgentFile(t, c, "reviewer", "---\nname: reviewer\ntitle: Reviewer\nreviews: true\n---\n")

	ghLog := writeFakeReviewBins(t, "diff --git a/f b/f\n+one line\n",
		`{"content":"{\"verdict\":\"approve\",\"comment\":\"meets all criteria\"}"}`)

	if !c.reviewPR("acme/backlog", 7) {
		t.Fatal("reviewPR() = false, want true for a completed approve review")
	}

	log, err := os.ReadFile(ghLog)
	if err != nil {
		t.Fatalf("read gh log: %v", err)
	}
	got := string(log)
	if !strings.Contains(got, "pr comment 7") {
		t.Errorf("expected a `gh pr comment` call for PR #7, log:\n%s", got)
	}
	if !strings.Contains(got, "pr merge 7 --squash --delete-branch") {
		t.Errorf("expected a `gh pr merge` call for PR #7 in merge=on mode, log:\n%s", got)
	}
}
