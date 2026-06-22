package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// githubBackend maps mago's task/HITL model onto GitHub issues:
//
//	task        = issue            status open      = open issue (no special label)
//	in_progress = label mago:in-progress + agent:<name>
//	blocked     = label mago:blocked
//	needs_human = label mago:hitl   (the question is a comment)
//	done        = closed issue
//	progress / HITL question / answer = issue comments
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
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Body   string `json:"body"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
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

func (gi ghIssue) project() string {
	for _, l := range gi.Labels {
		if strings.HasPrefix(l.Name, "project:") {
			return strings.TrimPrefix(l.Name, "project:")
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
		Project:  gi.project(),
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

func (b *githubBackend) viewIssue(number string) (ghIssue, error) {
	var gi ghIssue
	out, err := b.gh("issue", "view", number, "--json", "number,title,state,body,labels,comments")
	if err != nil {
		return gi, err
	}
	if err := json.Unmarshal([]byte(out), &gi); err != nil {
		return gi, fmt.Errorf("parse issue %s: %w", number, err)
	}
	return gi, nil
}

func (b *githubBackend) loadIssue(number string) (*Task, error) {
	gi, err := b.viewIssue(number)
	if err != nil {
		return nil, err
	}
	return gi.toTask(), nil
}

// isMagoComment reports whether a comment was posted by mago (vs a human reply).
// mago's comments start with a known marker; a human typing on GitHub does not.
func isMagoComment(body string) bool {
	t := strings.TrimSpace(body)
	for _, m := range []string{"🔧", "🙋", "📋", "↩", "**"} {
		if strings.HasPrefix(t, m) {
			return true
		}
	}
	return false
}

// resumeAnsweredHITL polls mago:hitl issues and, when the last comment is a human
// reply, flips the issue back to in-progress so the agent resumes. In production a
// webhook does this instantly; here we poll on each tick.
func (b *githubBackend) resumeAnsweredHITL() {
	issues, err := b.listIssues("--label", labHITL)
	if err != nil {
		return
	}
	for _, gi := range issues {
		full, err := b.viewIssue(strconv.Itoa(gi.Number))
		if err != nil || len(full.Comments) == 0 {
			continue
		}
		if last := full.Comments[len(full.Comments)-1]; !isMagoComment(last.Body) {
			b.gh("issue", "edit", strconv.Itoa(gi.Number), "--remove-label", labHITL, "--add-label", labInProgress)
		}
	}
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

func (b *githubBackend) AddTask(title, project string) (*Task, error) {
	b.ensureLabels()
	args := []string{"issue", "create", "--title", title, "--body", "Created via mago."}
	if project != "" {
		b.gh("label", "create", "project:"+project, "--color", "5319e7", "--force")
		args = append(args, "--label", "project:"+project)
	}
	out, err := b.gh(args...)
	if err != nil {
		return nil, err
	}
	num := issueNumberFromURL(strings.TrimSpace(out))
	return &Task{ID: num, Title: title, Status: "open", Project: project}, nil
}

func (b *githubBackend) PickActiveTask(agent string) (*Task, error) {
	b.resumeAnsweredHITL() // a human reply on a hitl issue makes it actionable again
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
	// an open issue routed to this agent
	for _, gi := range issues {
		if gi.status() == "open" && gi.assignee() == agent {
			return b.loadIssue(strconv.Itoa(gi.Number))
		}
	}
	// fallback: the first unrouted open issue
	for _, gi := range issues {
		if gi.status() == "open" && gi.assignee() == "" {
			return b.loadIssue(strconv.Itoa(gi.Number))
		}
	}
	return nil, nil
}

func (b *githubBackend) Assign(t *Task, agent string) error {
	b.ensureAgentLabel(agent)
	if _, err := b.gh("issue", "edit", t.ID, "--add-label", "agent:"+agent); err != nil {
		return err
	}
	t.Assignee = agent
	_, err := b.gh("issue", "comment", t.ID, "--body", "📋 routed to "+agent)
	return err
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

func (b *githubBackend) Bounce(t *Task) error {
	if t.Assignee != "" {
		b.gh("issue", "edit", t.ID, "--remove-label", "agent:"+t.Assignee)
	}
	b.gh("issue", "edit", t.ID, "--remove-label", labInProgress)
	t.Assignee = ""
	t.Status = "open"
	_, err := b.gh("issue", "comment", t.ID, "--body", "↩ Not the right role for this task — bouncing for re-routing.")
	return err
}

func (b *githubBackend) RecordProgress(t *Task, who, note string) error {
	// Header line identifies the agent (and marks it a mago comment for isMagoComment); the note
	// is structured markdown on its own lines (see progressNote) so the comment renders cleanly.
	_, err := b.gh("issue", "comment", t.ID, "--body", "**"+who+"** _(mago agent)_\n\n"+note)
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

// prShippedForTask reports whether an open or merged PR exists for this task's branch
// on the project repo — used to verify an implementer actually shipped before "done".
// Fails open (returns true) if it can't check, so transient errors never block a tick.
func prShippedForTask(projectRepo, taskID string) bool {
	out, err := gh("-R", projectRepo, "pr", "list", "--head", "mago/task-"+taskID, "--state", "all", "--json", "number")
	if err != nil {
		return true
	}
	var prs []struct {
		Number int `json:"number"`
	}
	if json.Unmarshal([]byte(out), &prs) != nil {
		return true
	}
	return len(prs) > 0
}

func issueNumberFromURL(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 {
		return url[i+1:]
	}
	return url
}
