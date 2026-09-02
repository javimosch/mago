package main

import (
	"os"
	"strings"
	"testing"
)

// TestGithubBackendListIssues_AppendsTaskLabel verifies listIssues merges any caller-supplied
// extra args with the configured taskLabel when both are present.
func TestGithubBackendListIssues_AppendsTaskLabel(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "list" ]; then
		printf '%s ' "$@" > "$0.args"
		echo '[{"number":7,"title":"scoped","state":"open","labels":[{"name":"mago:hitl"},{"name":"backlog"}]}]'
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web", taskLabel: "backlog"}
	issues, err := b.listIssues("--label", labHITL)
	if err != nil {
		t.Fatalf("listIssues: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 7 {
		t.Errorf("got %v, want 1 issue #7", issues)
	}

	got, err := os.ReadFile(fake + "/gh.args")
	if err != nil {
		t.Fatalf("expected gh call args: %v", err)
	}
	args := string(got)
	if !strings.Contains(args, "--label "+labHITL) {
		t.Errorf("gh args missing HITL label: %q", args)
	}
	if !strings.Contains(args, "--label backlog") {
		t.Errorf("gh args missing taskLabel: %q", args)
	}
}

// TestGithubBackendListIssues_GhError verifies a gh CLI failure is returned clearly.
func TestGithubBackendListIssues_GhError(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "list" ]; then
		echo "network timeout" >&2
		exit 1
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	_, err := b.listIssues()
	if err == nil {
		t.Fatal("expected error from listIssues")
	}
	if !strings.Contains(err.Error(), "issue list") || !strings.Contains(err.Error(), "network timeout") {
		t.Errorf("error %q should mention the failing subcommand and stderr", err.Error())
	}
}

// TestGithubBackendListIssues_Malformed verifies non-JSON output is reported as a parse error.
func TestGithubBackendListIssues_Malformed(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "list" ]; then
		echo "not json"
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	_, err := b.listIssues()
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse issue list") {
		t.Errorf("error %q does not mention parse issue list", err.Error())
	}
}
