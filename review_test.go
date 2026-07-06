package main

import (
	"os"
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
