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

func TestGithubBackendListTasksMalformed(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "list" ]; then echo "not json"; else exit 1; fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	_, err := b.ListTasks()
	if err == nil {
		t.Fatalf("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse issue list") {
		t.Errorf("error %q does not mention parse issue list", err.Error())
	}
}

func TestRepoOpenMagoPRs(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "pr" ] && [ "$4" = "list" ] && [ "${10}" = "headRefName" ]; then
		echo '[{"headRefName":"mago/task-1"},{"headRefName":"feature/x"},{"headRefName":"mago/task-2"}]'
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	if got := repoOpenMagoPRs("acme/web"); got != 2 {
		t.Errorf("repoOpenMagoPRs counted %d mago/ PRs, want 2", got)
	}
}

func TestRepoOpenMagoPRs_Failure(t *testing.T) {
	fake := fakeGh(t, `exit 1`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	if got := repoOpenMagoPRs("acme/web"); got != -1 {
		t.Errorf("repoOpenMagoPRs on gh failure = %d, want -1", got)
	}
}

func TestRepoOpenMagoPRs_MalformedJSON(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "pr" ] && [ "$4" = "list" ]; then echo "not json"; else exit 1; fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	if got := repoOpenMagoPRs("acme/web"); got != -1 {
		t.Errorf("repoOpenMagoPRs on malformed JSON = %d, want -1", got)
	}
}

func TestPrShippedForTask(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "pr" ] && [ "$4" = "list" ] && [ "$6" = "mago/task-123" ]; then
		echo '[{"number":5}]'
	else
		echo '[]'
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	if !prShippedForTask("acme/web", "123") {
		t.Error("prShippedForTask should report shipped for existing PR")
	}
	if prShippedForTask("acme/web", "999") {
		t.Error("prShippedForTask should report not shipped for missing PR")
	}
}

func TestPrShippedForTask_Failure(t *testing.T) {
	fake := fakeGh(t, `exit 1`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	if !prShippedForTask("acme/web", "123") {
		t.Error("prShippedForTask should fail open on gh error")
	}
}

func TestPrShippedForTask_MalformedJSON(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "pr" ] && [ "$4" = "list" ]; then echo "not json"; else exit 1; fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	if !prShippedForTask("acme/web", "123") {
		t.Error("prShippedForTask should fail open on malformed JSON")
	}
}

func TestGithubBackendFindTask(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "view" ]; then
		echo '{"number":7,"title":"find me","state":"open","body":"body","labels":[{"name":"agent:dev"},{"name":"project:web"}],"comments":[{"author":{"login":"qa"},"body":"ok"}]}'
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	task, err := b.FindTask("7")
	if err != nil {
		t.Fatalf("FindTask: %v", err)
	}
	if task.ID != "7" {
		t.Errorf("ID = %q, want 7", task.ID)
	}
	if task.Title != "find me" {
		t.Errorf("Title = %q, want find me", task.Title)
	}
	if task.Assignee != "dev" {
		t.Errorf("Assignee = %q, want dev", task.Assignee)
	}
	if task.Project != "web" {
		t.Errorf("Project = %q, want web", task.Project)
	}
	if !strings.Contains(task.Body, "### qa") || !strings.Contains(task.Body, "ok") {
		t.Errorf("Body missing expected comment text: %q", task.Body)
	}
}

func TestGithubBackendViewIssueMalformed(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "view" ]; then echo "not json"; else exit 1; fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	_, err := b.viewIssue("8")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse issue 8") {
		t.Errorf("error %q does not mention parse issue 8", err.Error())
	}
}

func TestGithubBackendPendingHITL(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "list" ]; then
		echo '[{"number":5,"title":"blocked on you","state":"open","labels":[{"name":"mago:hitl"}]}]'
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	got, err := b.PendingHITL()
	if err != nil {
		t.Fatalf("PendingHITL: %v", err)
	}
	if len(got) != 1 || got[0] != "#5 blocked on you" {
		t.Errorf("PendingHITL = %v, want [#5 blocked on you]", got)
	}
}

func TestGithubBackendAddTask(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "label" ]; then
		exit 0
	elif [ "$3" = "issue" ] && [ "$4" = "create" ]; then
		echo "https://github.com/acme/web/issues/9"
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	task, err := b.AddTask("new task", "supercli")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if task.ID != "9" {
		t.Errorf("ID = %q, want 9", task.ID)
	}
	if task.Title != "new task" {
		t.Errorf("Title = %q, want new task", task.Title)
	}
	if task.Project != "supercli" {
		t.Errorf("Project = %q, want supercli", task.Project)
	}
	if task.Status != "open" {
		t.Errorf("Status = %q, want open", task.Status)
	}
}

func TestGithubBackendAddTaskWithTaskLabel(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "label" ] && [ "$4" = "create" ]; then
		printf '%s\n' "$*" >> "$0.labels"
		exit 0
	elif [ "$3" = "issue" ] && [ "$4" = "create" ]; then
		printf '%s\n' "$*" >> "$0.create"
		echo "https://github.com/acme/web/issues/9"
		exit 0
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web", taskLabel: "mago:backlog"}
	task, err := b.AddTask("new task", "supercli")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if task.ID != "9" {
		t.Errorf("ID = %q, want 9", task.ID)
	}

	labels, err := os.ReadFile(fake + "/gh.labels")
	if err != nil {
		t.Fatalf("reading recorded labels: %v", err)
	}
	if !strings.Contains(string(labels), "mago:backlog") || !strings.Contains(string(labels), "project:supercli") {
		t.Errorf("labels missing expected entries: %q", labels)
	}

	create, err := os.ReadFile(fake + "/gh.create")
	if err != nil {
		t.Fatalf("reading recorded create args: %v", err)
	}
	s := string(create)
	if !strings.Contains(s, "--label") || !strings.Contains(s, "mago:backlog") || !strings.Contains(s, "project:supercli") {
		t.Errorf("create args missing expected labels: %q", s)
	}
}

func TestGithubBackendAddTaskCreateError(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "create" ]; then
		echo "create failed" >&2
		exit 1
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	_, err := b.AddTask("new task", "")
	if err == nil {
		t.Fatal("expected error from AddTask")
	}
	if !strings.Contains(err.Error(), "create failed") {
		t.Errorf("error = %q, want to contain 'create failed'", err.Error())
	}
}

func TestGhWrapperPassesToken(t *testing.T) {
	fake := fakeGh(t, `if [ "$1" = "issue" ] && [ "$2" = "list" ]; then
		echo "GH_TOKEN=${GH_TOKEN}"
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
	t.Setenv("MAGO_GH_TOKEN", "tok_123")

	out, err := gh("issue", "list")
	if err != nil {
		t.Fatalf("gh: %v", err)
	}
	if !strings.Contains(out, "GH_TOKEN=tok_123") {
		t.Errorf("gh did not pass GH_TOKEN; got %q", out)
	}
}

func TestGhWrapperNonAuthError(t *testing.T) {
	fake := fakeGh(t, `if [ "$1" = "issue" ] && [ "$2" = "view" ]; then
		echo "network timeout" >&2
		exit 1
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	_, err := gh("issue", "view", "1")
	if err == nil {
		t.Fatal("expected error from gh")
	}
	want := "gh issue view 1: network timeout"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestGithubBackendSetStatus(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ]; then
		exit 0
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	cases := []struct {
		status string
		want   string
	}{
		{"done", "done"},
		{"blocked", "blocked"},
		{"in_progress", "in_progress"},
		{"anything", "in_progress"},
	}
	for _, c := range cases {
		t.Run(c.status, func(t *testing.T) {
			task := &Task{ID: "7", Status: "open"}
			if err := b.SetStatus(task, c.status); err != nil {
				t.Fatalf("SetStatus(%q): %v", c.status, err)
			}
			if task.Status != c.want {
				t.Errorf("status = %q, want %q", task.Status, c.want)
			}
		})
	}
}

func TestGithubBackendRecordProgress(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "comment" ]; then
		printf '%s' "$7" > "$0.body"
		exit 0
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	task := &Task{ID: "7"}
	if err := b.RecordProgress(task, "cto", "made progress"); err != nil {
		t.Fatalf("RecordProgress: %v", err)
	}
	body, err := os.ReadFile(fake + "/gh.body")
	if err != nil {
		t.Fatalf("reading recorded body: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "**cto** _(mago agent)_") || !strings.Contains(got, "made progress") {
		t.Errorf("comment body missing expected text: %q", got)
	}
}

func TestGithubBackendEnsureAgentLabel(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "label" ] && [ "$4" = "create" ]; then
		printf '%s' "$5" > "$0.label"
		exit 0
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	b.ensureAgentLabel("dev")

	got, err := os.ReadFile(fake + "/gh.label")
	if err != nil {
		t.Fatalf("expected ensureAgentLabel to create an agent label: %v", err)
	}
	if string(got) != "agent:dev" {
		t.Errorf("label name = %q, want agent:dev", got)
	}
}

func TestGithubBackendBounce(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "edit" ]; then
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
	task := &Task{ID: "7", Assignee: "dev", Status: "in_progress"}
	if err := b.Bounce(task); err != nil {
		t.Fatalf("Bounce: %v", err)
	}
	if task.Assignee != "" {
		t.Errorf("Assignee = %q, want empty", task.Assignee)
	}
	if task.Status != "open" {
		t.Errorf("Status = %q, want open", task.Status)
	}
	edits, err := os.ReadFile(fake + "/gh.edits")
	if err != nil {
		t.Fatalf("reading recorded edits: %v", err)
	}
	if !strings.Contains(string(edits), "agent:dev") || !strings.Contains(string(edits), labInProgress) {
		t.Errorf("edits missing expected labels: %q", edits)
	}
	body, err := os.ReadFile(fake + "/gh.body")
	if err != nil {
		t.Fatalf("reading recorded body: %v", err)
	}
	if !strings.Contains(string(body), "bouncing") {
		t.Errorf("comment body missing bounce text: %q", body)
	}
}

func TestGithubBackendClearClarify(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "edit" ]; then
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
	task := &Task{ID: "7", Assignee: "planner", Status: "needs_human"}
	if err := b.ClearClarify(task); err != nil {
		t.Fatalf("ClearClarify: %v", err)
	}
	if task.Assignee != "" {
		t.Errorf("Assignee = %q, want empty", task.Assignee)
	}
	if task.Status != "open" {
		t.Errorf("Status = %q, want open", task.Status)
	}
	edits, err := os.ReadFile(fake + "/gh.edits")
	if err != nil {
		t.Fatalf("reading recorded edits: %v", err)
	}
	s := string(edits)
	if !strings.Contains(s, labClarify) || !strings.Contains(s, labHITL) || !strings.Contains(s, labInProgress) || !strings.Contains(s, "agent:planner") {
		t.Errorf("edits missing expected labels: %q", s)
	}
	body, err := os.ReadFile(fake + "/gh.body")
	if err != nil {
		t.Fatalf("reading recorded body: %v", err)
	}
	if !strings.Contains(string(body), "mago:go") {
		t.Errorf("comment body missing mago:go text: %q", body)
	}
}
