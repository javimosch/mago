package main

import (
	"os"
	"strings"
	"testing"
)

func TestGithubBackendViewIssueCommandFailure(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "view" ]; then
		echo "boom" >&2
		exit 1
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	_, err := b.viewIssue("8")
	if err == nil {
		t.Fatal("expected error when gh view fails")
	}
	if !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "issue view") {
		t.Errorf("error = %q, want gh issue view error containing 'boom'", err.Error())
	}
}
