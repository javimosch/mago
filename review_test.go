package main

import (
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
