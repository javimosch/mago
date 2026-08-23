package main

import (
	"os"
	"strings"
	"testing"
)

func TestGithubBackendAssign(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "label" ] && [ "$4" = "create" ]; then
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
	if err := b.Assign(task, "dev"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if task.Assignee != "dev" {
		t.Errorf("Assignee = %q, want dev", task.Assignee)
	}
	if task.Status != "open" {
		t.Errorf("Status = %q, want open", task.Status)
	}

	edits, err := os.ReadFile(fake + "/gh.edits")
	if err != nil {
		t.Fatalf("reading recorded edits: %v", err)
	}
	if !strings.Contains(string(edits), "agent:dev") {
		t.Errorf("edits missing expected agent label: %q", edits)
	}

	body, err := os.ReadFile(fake + "/gh.body")
	if err != nil {
		t.Fatalf("reading recorded body: %v", err)
	}
	if !strings.Contains(string(body), "routed to dev") {
		t.Errorf("comment body missing expected text: %q", body)
	}
}

func TestGithubBackendAssignIssueEditFailure(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "label" ] && [ "$4" = "create" ]; then
		exit 0
	elif [ "$3" = "issue" ] && [ "$4" = "edit" ]; then
		echo "edit failed" >&2
		exit 1
	elif [ "$3" = "issue" ] && [ "$4" = "comment" ]; then
		echo "should not reach comment" > "$0.body"
		exit 0
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	task := &Task{ID: "7", Status: "open"}
	err := b.Assign(task, "dev")
	if err == nil {
		t.Fatal("expected error when gh issue edit fails")
	}
	if !strings.Contains(err.Error(), "edit failed") {
		t.Errorf("error = %q, want gh issue edit error containing 'edit failed'", err.Error())
	}
	if _, statErr := os.Stat(fake + "/gh.body"); !os.IsNotExist(statErr) {
		t.Error("comment should not be posted when issue edit fails")
	}
}
