package main

import (
	"os"
	"testing"
)

func TestGithubBackendPickActiveTaskResumesInProgress(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "list" ]; then
		case "$*" in
			*mago:hitl*) echo '[]'; exit 0 ;;
		esac
		echo '[{"number":7,"title":"pick me","state":"open","labels":[{"name":"mago:in-progress"},{"name":"agent:dev"}]}]'
		exit 0
	elif [ "$3" = "issue" ] && [ "$4" = "view" ] && [ "$5" = "7" ]; then
		echo '{"number":7,"title":"pick me","state":"open","body":"do it","labels":[{"name":"mago:in-progress"},{"name":"agent:dev"}],"comments":[]}'
		exit 0
	fi
	exit 1`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	task, err := b.PickActiveTask("dev")
	if err != nil {
		t.Fatalf("PickActiveTask: %v", err)
	}
	if task == nil {
		t.Fatal("PickActiveTask returned nil")
	}
	if task.ID != "7" {
		t.Errorf("ID = %q, want 7", task.ID)
	}
	if task.Status != "in_progress" {
		t.Errorf("Status = %q, want in_progress", task.Status)
	}
	if task.Assignee != "dev" {
		t.Errorf("Assignee = %q, want dev", task.Assignee)
	}
}

func TestGithubBackendPickActiveTaskFallsBackToUnrouted(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "list" ]; then
		case "$*" in
			*mago:hitl*) echo '[]'; exit 0 ;;
		esac
		echo '[{"number":8,"title":"open task","state":"open","labels":[]}]'
		exit 0
	elif [ "$3" = "issue" ] && [ "$4" = "view" ] && [ "$5" = "8" ]; then
		echo '{"number":8,"title":"open task","state":"open","body":"do it","labels":[],"comments":[]}'
		exit 0
	fi
	exit 1`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	task, err := b.PickActiveTask("dev")
	if err != nil {
		t.Fatalf("PickActiveTask: %v", err)
	}
	if task == nil {
		t.Fatal("PickActiveTask returned nil")
	}
	if task.ID != "8" {
		t.Errorf("ID = %q, want 8", task.ID)
	}
	if task.Status != "open" {
		t.Errorf("Status = %q, want open", task.Status)
	}
	if task.Assignee != "" {
		t.Errorf("Assignee = %q, want empty", task.Assignee)
	}
}

func TestGithubBackendPickActiveTaskEmptyQueue(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "list" ]; then
		echo '[]'
		exit 0
	fi
	exit 1`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	task, err := b.PickActiveTask("dev")
	if err != nil {
		t.Fatalf("PickActiveTask: %v", err)
	}
	if task != nil {
		t.Errorf("PickActiveTask = %v, want nil", task)
	}
}
