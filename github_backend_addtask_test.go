package main

import (
	"os"
	"strings"
	"testing"
)

func TestGithubBackendAddTaskWithTaskLabel(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "label" ] && [ "$4" = "create" ]; then
		exit 0
	elif [ "$3" = "issue" ] && [ "$4" = "create" ]; then
		printf '%s\n' "$*" >> "$0.create"
		echo "https://github.com/acme/web/issues/11"
		exit 0
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web", taskLabel: "mago:task"}
	task, err := b.AddTask("scoped task", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if task.ID != "11" {
		t.Errorf("ID = %q, want 11", task.ID)
	}
	if task.Status != "open" {
		t.Errorf("Status = %q, want open", task.Status)
	}

	create, err := os.ReadFile(fake + "/gh.create")
	if err != nil {
		t.Fatalf("expected issue create args to be recorded: %v", err)
	}
	if !strings.Contains(string(create), b.taskLabel) {
		t.Errorf("issue create args missing task label %q: %q", b.taskLabel, create)
	}
}
