package main

import (
	"os"
	"strings"
	"testing"
)

func TestGithubBackendRecordProgressCommentFailure(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "comment" ]; then
		echo "comment failed" >&2
		exit 1
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	task := &Task{ID: "7"}
	err := b.RecordProgress(task, "cto", "made progress")
	if err == nil {
		t.Fatal("expected error when gh issue comment fails")
	}
	if !strings.Contains(err.Error(), "comment failed") {
		t.Errorf("error = %q, want gh issue comment error containing 'comment failed'", err.Error())
	}
	if _, statErr := os.Stat(fake + "/gh.body"); !os.IsNotExist(statErr) {
		t.Error("comment body should not be written when gh issue comment fails")
	}
}
