package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TaskBackend is the "world" mago coordinates through: tasks, claims, progress,
// status, and HITL. Local files for the POC; GitHub (issues/comments) for the product.
type TaskBackend interface {
	AddTask(title string) (*Task, error)
	ListTasks() ([]*Task, error)
	FindTask(id string) (*Task, error)
	PickActiveTask(agent string) (*Task, error)
	Assign(t *Task, agent string) error // route an open task to an owner without starting it
	Claim(t *Task, agent string) error
	RecordProgress(t *Task, who, note string) error
	SetStatus(t *Task, status string) error
	RaiseHITL(t *Task, agent, question string) error
	AnswerHITL(id, text string) error
	PendingHITL() ([]string, error)
}

// ---------- local (file) backend ----------

type localBackend struct{ c *Company }

func (b *localBackend) taskPath(id string) string {
	return filepath.Join(b.c.tasksDir(), "task-"+id+".md")
}

func (b *localBackend) save(t *Task) error {
	fm := map[string]string{"id": t.ID, "title": t.Title, "status": t.Status, "assignee": t.Assignee, "claimed_at": t.ClaimedAt}
	order := []string{"id", "title", "status", "assignee", "claimed_at"}
	return os.WriteFile(b.taskPath(t.ID), []byte(renderFrontmatter(fm, order, t.Body)), 0o644)
}

func (b *localBackend) ListTasks() ([]*Task, error) {
	entries, err := os.ReadDir(b.c.tasksDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var tasks []*Task
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		bts, err := os.ReadFile(filepath.Join(b.c.tasksDir(), e.Name()))
		if err != nil {
			continue
		}
		fm, body := parseFrontmatter(string(bts))
		tasks = append(tasks, &Task{
			ID:        orDefault(fm["id"], strings.TrimSuffix(e.Name(), ".md")),
			Title:     fm["title"],
			Status:    orDefault(fm["status"], "open"),
			Assignee:  fm["assignee"],
			ClaimedAt: fm["claimed_at"],
			Body:      strings.TrimSpace(body),
		})
	}
	sort.Slice(tasks, func(i, j int) bool { return atoiSafe(tasks[i].ID) < atoiSafe(tasks[j].ID) })
	return tasks, nil
}

func (b *localBackend) FindTask(id string) (*Task, error) {
	ts, err := b.ListTasks()
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("task #%s not found", id)
}

func (b *localBackend) AddTask(title string) (*Task, error) {
	ts, _ := b.ListTasks()
	maxID := 0
	for _, t := range ts {
		if n := atoiSafe(t.ID); n > maxID {
			maxID = n
		}
	}
	t := &Task{ID: fmtID(maxID + 1), Title: title, Status: "open", Body: "## Description\n" + title + "\n\n## Progress log\n"}
	return t, b.save(t)
}

func (b *localBackend) PickActiveTask(agent string) (*Task, error) {
	ts, err := b.ListTasks()
	if err != nil {
		return nil, err
	}
	for _, t := range ts { // resume my in-progress work
		if t.Status == "in_progress" && (t.Assignee == agent || t.Assignee == "") {
			return t, nil
		}
	}
	for _, t := range ts { // a task routed to me
		if t.Status == "open" && t.Assignee == agent {
			return t, nil
		}
	}
	for _, t := range ts { // fallback: an unrouted open task
		if t.Status == "open" && t.Assignee == "" {
			return t, nil
		}
	}
	return nil, nil
}

func (b *localBackend) Assign(t *Task, agent string) error {
	t.Assignee = agent
	return b.save(t)
}

func (b *localBackend) Claim(t *Task, agent string) error {
	t.Status = "in_progress"
	t.Assignee = agent
	t.ClaimedAt = nowStamp()
	return b.save(t)
}

func (b *localBackend) RecordProgress(t *Task, who, note string) error {
	t.Body += progressEntry(nowStamp(), who, note)
	return b.save(t)
}

func (b *localBackend) SetStatus(t *Task, status string) error {
	t.Status = status
	return b.save(t)
}

func (b *localBackend) RaiseHITL(t *Task, agent, question string) error {
	ensureDir(b.c.inboxDir())
	body := fmt.Sprintf("from: %s\ntask: #%s %s\nat: %s\n\nQUESTION:\n%s\n", agent, t.ID, t.Title, nowStamp(), question)
	os.WriteFile(filepath.Join(b.c.inboxDir(), "task-"+t.ID+".md"), []byte(body), 0o644)
	t.Status = "needs_human"
	t.Body += progressEntry(nowStamp(), agent, "NEEDS HUMAN: "+question)
	return b.save(t)
}

func (b *localBackend) AnswerHITL(id, text string) error {
	t, err := b.FindTask(id)
	if err != nil {
		return err
	}
	t.Body += progressEntry(nowStamp(), "ceo", "HUMAN ANSWER: "+text)
	t.Status = "in_progress"
	if err := b.save(t); err != nil {
		return err
	}
	os.Remove(filepath.Join(b.c.inboxDir(), "task-"+id+".md"))
	return nil
}

func (b *localBackend) PendingHITL() ([]string, error) {
	entries, _ := os.ReadDir(b.c.inboxDir())
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			bts, _ := os.ReadFile(filepath.Join(b.c.inboxDir(), e.Name()))
			out = append(out, string(bts))
		}
	}
	return out, nil
}
