package main

import (
	"os"
	"strings"
	"testing"
)

func TestGithubBackendResumeAnsweredHITL(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "list" ]; then
		echo '[{"number":7,"title":"hitl me","state":"open","labels":[{"name":"mago:hitl"}]}]'
	elif [ "$3" = "issue" ] && [ "$4" = "view" ]; then
		echo '{"number":7,"title":"hitl me","state":"open","labels":[{"name":"mago:hitl"}],"comments":[{"author":{"login":"ceo"},"body":"yes, do it"}]}'
	elif [ "$3" = "issue" ] && [ "$4" = "edit" ]; then
		printf '%s\n' "$*" >> "$0.edits"
		exit 0
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	b.resumeAnsweredHITL()

	edits, err := os.ReadFile(fake + "/gh.edits")
	if err != nil {
		t.Fatalf("expected issue edit for resumed HITL: %v", err)
	}
	s := string(edits)
	if !strings.Contains(s, labHITL) || !strings.Contains(s, labInProgress) {
		t.Errorf("edits missing expected label changes: %q", s)
	}
}

// TestGithubBackendResumeAnsweredHITL_ListError verifies resumeAnsweredHITL aborts
// quietly when the initial issue list command fails, without editing any issue.
func TestGithubBackendResumeAnsweredHITL_ListError(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "list" ]; then
		echo "network timeout" >&2
		exit 1
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	b.resumeAnsweredHITL()

	if _, err := os.Stat(fake + "/gh.edits"); !os.IsNotExist(err) {
		t.Errorf("resumeAnsweredHITL should not edit issues when listIssues fails")
	}
}
