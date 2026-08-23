package main

import (
	"os"
	"strings"
	"testing"
)

func TestGithubBackendClaim(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "label" ] && [ "$4" = "create" ]; then
		printf '%s\n' "$*" >> "$0.labels"
		exit 0
	elif [ "$3" = "issue" ] && [ "$4" = "edit" ]; then
		printf '%s\n' "$*" >> "$0.edits"
		exit 0
	elif [ "$3" = "issue" ] && [ "$4" = "comment" ]; then
		printf '%s' "$7" > "$0.body"
		exit 0
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	task := &Task{ID: "7", Status: "open"}
	if err := b.Claim(task, "dev"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if task.Assignee != "dev" {
		t.Errorf("Assignee = %q, want dev", task.Assignee)
	}
	if task.Status != "in_progress" {
		t.Errorf("Status = %q, want in_progress", task.Status)
	}

	labels, err := os.ReadFile(fake + "/gh.labels")
	if err != nil {
		t.Fatalf("reading recorded labels: %v", err)
	}
	if !strings.Contains(string(labels), labInProgress) {
		t.Errorf("labels missing %q: %q", labInProgress, labels)
	}

	edits, err := os.ReadFile(fake + "/gh.edits")
	if err != nil {
		t.Fatalf("reading recorded edits: %v", err)
	}
	s := string(edits)
	if !strings.Contains(s, labInProgress) || !strings.Contains(s, "agent:dev") {
		t.Errorf("edits missing expected labels: %q", edits)
	}

	body, err := os.ReadFile(fake + "/gh.body")
	if err != nil {
		t.Fatalf("reading recorded body: %v", err)
	}
	if !strings.Contains(string(body), "dev picking this up") {
		t.Errorf("comment body missing expected text: %q", body)
	}
}
