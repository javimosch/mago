package main

import (
	"os"
	"strings"
	"testing"
)

func TestGithubBackendRaiseHITL(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "comment" ]; then
		printf '%s' "$7" > "$0.body"
		exit 0
	elif [ "$3" = "issue" ] && [ "$4" = "edit" ]; then
		printf '%s\n' "$*" >> "$0.edits"
		exit 0
	fi
	exit 1`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	task := &Task{ID: "7", Status: "in_progress"}
	if err := b.RaiseHITL(task, "dev", "what should I do?"); err != nil {
		t.Fatalf("RaiseHITL: %v", err)
	}
	if task.Status != "needs_human" {
		t.Errorf("Status = %q, want needs_human", task.Status)
	}

	body, err := os.ReadFile(fake + "/gh.body")
	if err != nil {
		t.Fatalf("reading recorded body: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "dev") || !strings.Contains(got, "what should I do?") {
		t.Errorf("comment body missing expected text: %q", got)
	}

	edits, err := os.ReadFile(fake + "/gh.edits")
	if err != nil {
		t.Fatalf("reading recorded edits: %v", err)
	}
	s := string(edits)
	if !strings.Contains(s, labHITL) || !strings.Contains(s, labInProgress) {
		t.Errorf("edits missing expected labels: %q", s)
	}
}
