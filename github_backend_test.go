package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestIssueNumberFromURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://github.com/owner/repo/issues/42", "42"},
		{"https://github.com/owner/repo/issues/7#comment", "7#comment"},
		{"123", "123"},
		{"", ""},
	}
	for _, c := range cases {
		got := issueNumberFromURL(c.in)
		if got != c.want {
			t.Errorf("issueNumberFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsMagoComment(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"🔧 agent picking this up", true},
		{"🙋 **cto** needs the CEO:", true},
		{"📋 routed to cto", true},
		{"↩ Not the right role", true},
		{"📣 shipped", true},
		{"**agent** _(mago agent)_", true},
		{"human reply here", false},
		{"  **bold** start with spaces", true},
		{"", false},
	}
	for _, c := range cases {
		got := isMagoComment(c.in)
		if got != c.want {
			t.Errorf("isMagoComment(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestGhIssueToTask(t *testing.T) {
	payload := []byte(`{"number":42,"title":"ship it","state":"open","body":"do the thing","labels":[{"name":"mago:in-progress"},{"name":"agent:cto"}],"comments":[{"author":{"login":"dev1"},"body":"progress"}]}`)
	var gi ghIssue
	if err := json.Unmarshal(payload, &gi); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	task := gi.toTask()
	if task.ID != "42" {
		t.Errorf("ID = %q, want 42", task.ID)
	}
	if task.Title != "ship it" {
		t.Errorf("Title = %q, want ship it", task.Title)
	}
	if task.Assignee != "cto" {
		t.Errorf("Assignee = %q, want cto", task.Assignee)
	}
	if !strings.Contains(task.Body, "## Progress log") {
		t.Errorf("Body missing progress log header: %q", task.Body)
	}
	if !strings.Contains(task.Body, "### dev1") {
		t.Errorf("Body missing comment author: %q", task.Body)
	}
	if !strings.Contains(task.Body, "progress") {
		t.Errorf("Body missing comment text: %q", task.Body)
	}
}

func TestGhIssueMethods(t *testing.T) {
	payload := []byte(`{"number":1,"title":"test","state":"open","labels":[{"name":"mago:in-progress"},{"name":"agent:cto"},{"name":"project:supercli"}]}`)
	var gi ghIssue
	if err := json.Unmarshal(payload, &gi); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !gi.hasLabel("mago:in-progress") {
		t.Error("hasLabel: expected to find mago:in-progress")
	}
	if gi.hasLabel("mago:blocked") {
		t.Error("hasLabel: unexpected mago:blocked")
	}
	if gi.assignee() != "cto" {
		t.Errorf("assignee() = %q, want cto", gi.assignee())
	}
	if gi.project() != "supercli" {
		t.Errorf("project() = %q, want supercli", gi.project())
	}
	if gi.status() != "in_progress" {
		t.Errorf("status() = %q, want in_progress", gi.status())
	}
}

func TestGhIssueStatusClosed(t *testing.T) {
	payload := []byte(`{"number":2,"title":"closed","state":"CLOSED","labels":[{"name":"mago:in-progress"}]}`)
	var gi ghIssue
	if err := json.Unmarshal(payload, &gi); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gi.status() != "done" {
		t.Errorf("status() = %q, want done", gi.status())
	}
}

func TestGhIssueEmptyLabels(t *testing.T) {
	payload := []byte(`{"number":3,"title":"plain","state":"open","labels":[]}`)
	var gi ghIssue
	if err := json.Unmarshal(payload, &gi); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gi.status() != "open" {
		t.Errorf("status() = %q, want open", gi.status())
	}
	if gi.assignee() != "" {
		t.Errorf("assignee() = %q, want empty", gi.assignee())
	}
	if gi.project() != "" {
		t.Errorf("project() = %q, want empty", gi.project())
	}
}

func TestGithubBackendListTasks(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "list" ]; then echo '[{"number":42,"title":"ship it","state":"open","labels":[{"name":"mago:in-progress"},{"name":"agent:cto"},{"name":"project:supercli"}]}]'; else exit 1; fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	tasks, err := b.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	task := tasks[0]
	if task.ID != "42" {
		t.Errorf("ID = %q, want 42", task.ID)
	}
	if task.Title != "ship it" {
		t.Errorf("Title = %q, want ship it", task.Title)
	}
	if task.Status != "in_progress" {
		t.Errorf("Status = %q, want in_progress", task.Status)
	}
	if task.Assignee != "cto" {
		t.Errorf("Assignee = %q, want cto", task.Assignee)
	}
	if task.Project != "supercli" {
		t.Errorf("Project = %q, want supercli", task.Project)
	}
}
