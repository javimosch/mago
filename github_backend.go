package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// githubBackend maps mago's task/HITL model onto GitHub issues:
//   task        = issue            status open      = open issue (no special label)
//   in_progress = label mago:in-progress + agent:<name>
//   blocked     = label mago:blocked
//   needs_human = label mago:hitl   (the question is a comment)
//   done        = closed issue
//   progress / HITL question / answer = issue comments
type githubBackend struct{ repo string }

const (
	labInProgress = "mago:in-progress"
	labBlocked    = "mago:blocked"
	labHITL       = "mago:hitl"
)

func gh(args ...string) (string, error) {
	out, err := exec.Command("gh", args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("gh %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

func (b *githubBackend) gh(args ...string) (string, error) {
	return gh(append([]string{"-R", b.repo}, args...)...)
}

func (b *githubBackend) ensureLabels() {
	for _, l := range []struct{ name, color string }{
		{labInProgress, "1d76db"}, {labBlocked, "b60205"}, {labHITL, "fbca04"},
	} {
		b.gh("label", "create", l.name, "--color", l.color, "--force")
	}
}

func (b *githubBackend) ensureAgentLabel(agent string) {
	b.gh("label", "create", "agent:"+agent, "--color", "0e8a16", "--force")
}

type ghIssue struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	State    string `json:"state"`
	Body     string `json:"body"`
	Labels   []struct{ Name string `json:"name"` } `json:"labels"`
	Comments []struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Body string `json:"body"`
	} `json:"comments"`
}

func (gi ghIssue) hasLabel(name string) bool {
	for _, l := range gi.Labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

func (gi ghIssue) status() string {
	if strings.EqualFold(gi.State, "closed") {
		return "done"
	}
	switch {
	case gi.hasLabel(labHITL):
		return "needs_human"
	case gi.hasLabel(labBlocked):
		return "blocked"
	case gi.hasLabel(labInProgress):
		return "in_progress"
	}
	return "open"
}

func (gi ghIssue) assignee() string {
	for _, l := range gi.Labels {
		if strings.HasPrefix(l.Name, "agent:") {
			return strings.TrimPrefix(l.Name, "agent:")
		}
	}
	return ""
}

// toTask builds a Task; includes body + comments as the progress log.
func (gi ghIssue) toTask() *Task {
	body := gi.Body + "\n\n## Progress log\n"
	for _, c := range gi.Comments {
		body += "\n### " + c.Author.Login + "\n" + c.Body + "\n"
	}
	return &Task{
		ID:       strconv.Itoa(gi.Number),
		Title:    gi.Title,
		Status:   gi.status(),
		Assignee: gi.assignee(),
		Body:     strings.TrimSpace(body),
	}
}

func (b *githubBackend) listIssues(extra ...string) ([]ghIssue, error) {
	args := append([]string{"issue", "list", "--state", "all", "--limit", "200",
		"--json", "number,title,state,labels"}, extra...)
	out, err := b.gh(args...)
	if err != nil {
		return nil, err
	}
	var issues []ghIssue
	if err := json.Unmarshal([]byte(out), &issues); err != nil {
		return nil, fmt.Errorf("parse issue list: %w", err)
	}
	return issues, nil
}

func (b *githubBackend) loadIssue(number string) (*Task, error) {
	out, err := b.gh("issue", "view", number, "--json", "number,title,state,body,labels,comments")
	if err != nil {
		return nil, err
	}
	var gi ghIssue
	if err := json.Unmarshal([]byte(out), &gi); err != nil {
		return nil, fmt.Errorf("parse issue %s: %w", number, err)
	}
	return gi.toTask(), nil
}

func (b *githubBackend) ListTasks() ([]*Task, error) {
	issues, err := b.listIssues()
	if err != nil {
		return nil, err
	}
	var tasks []*Task
	for _, gi := range issues {
		tasks = append(tasks, gi.toTask())
	}
	return tasks, nil
}

func (b *githubBackend) FindTask(id string) (*Task, error) { return b.loadIssue(id) }

func (b *githubBackend) AddTask(title string) (*Task, error) {
	b.ensureLabels()
	out, err := b.gh("issue", "create", "--title", title, "--body", "Created via mago.")
	if err != nil {
		return nil, err
	}
	num := issueNumberFromURL(strings.TrimSpace(out))
	return &Task{ID: num, Title: title, Status: "open"}, nil
}

func (b *githubBackend) PickActiveTask(agent string) (*Task, error) {
	issues, err := b.listIssues()
	if err != nil {
		return nil, err
	}
	// resume an in-progress issue this agent already owns
	for _, gi := range issues {
		if gi.status() == "in_progress" && (gi.assignee() == agent || gi.assignee() == "") {
			return b.loadIssue(strconv.Itoa(gi.Number))
		}
	}
	// else claim the first plain-open issue (not HITL/in-progress/blocked/done)
	for _, gi := range issues {
		if gi.status() == "open" {
			return b.loadIssue(strconv.Itoa(gi.Number))
		}
	}
	return nil, nil
}

func (b *githubBackend) Claim(t *Task, agent string) error {
	b.ensureLabels()
	b.ensureAgentLabel(agent)
	if _, err := b.gh("issue", "edit", t.ID, "--add-label", labInProgress, "--add-label", "agent:"+agent); err != nil {
		return err
	}
	t.Status = "in_progress"
	t.Assignee = agent
	_, err := b.gh("issue", "comment", t.ID, "--body", "🔧 "+agent+" picking this up")
	return err
}

func (b *githubBackend) RecordProgress(t *Task, who, note string) error {
	_, err := b.gh("issue", "comment", t.ID, "--body", "**"+who+":** "+note)
	return err
}

func (b *githubBackend) SetStatus(t *Task, status string) error {
	switch status {
	case "done":
		_, err := b.gh("issue", "close", t.ID)
		t.Status = "done"
		return err
	case "blocked":
		_, err := b.gh("issue", "edit", t.ID, "--add-label", labBlocked, "--remove-label", labInProgress)
		t.Status = "blocked"
		return err
	default:
		_, err := b.gh("issue", "edit", t.ID, "--add-label", labInProgress)
		t.Status = "in_progress"
		return err
	}
}

func (b *githubBackend) RaiseHITL(t *Task, agent, question string) error {
	if _, err := b.gh("issue", "comment", t.ID, "--body", "🙋 **"+agent+" needs the CEO:** "+question); err != nil {
		return err
	}
	_, err := b.gh("issue", "edit", t.ID, "--add-label", labHITL, "--remove-label", labInProgress)
	t.Status = "needs_human"
	return err
}

func (b *githubBackend) AnswerHITL(id, text string) error {
	if _, err := b.gh("issue", "comment", id, "--body", "**CEO:** "+text); err != nil {
		return err
	}
	_, err := b.gh("issue", "edit", id, "--remove-label", labHITL, "--add-label", labInProgress)
	return err
}

func (b *githubBackend) PendingHITL() ([]string, error) {
	issues, err := b.listIssues("--label", labHITL)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, gi := range issues {
		out = append(out, fmt.Sprintf("#%d %s", gi.Number, gi.Title))
	}
	return out, nil
}

func issueNumberFromURL(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 {
		return url[i+1:]
	}
	return url
}
