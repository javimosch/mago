package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Company is a local mago company rooted at a directory containing .mago/.
type Company struct {
	Dir  string
	Name string
}

func loadCompany(dir string) (*Company, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(abs, ".mago")); err != nil {
		return nil, fmt.Errorf("not a mago company (no .mago/) at %s — run `mago init` first", abs)
	}
	return &Company{Dir: abs, Name: filepath.Base(abs)}, nil
}

func (c *Company) magoDir() string      { return filepath.Join(c.Dir, ".mago") }
func (c *Company) agentsDir() string    { return filepath.Join(c.magoDir(), "agents") }
func (c *Company) skillsDir() string    { return filepath.Join(c.magoDir(), "skills") }
func (c *Company) runsDir() string      { return filepath.Join(c.magoDir(), "runs") }
func (c *Company) inboxDir() string     { return filepath.Join(c.magoDir(), "inbox") }
func (c *Company) tasksDir() string     { return filepath.Join(c.Dir, "tasks") }
func (c *Company) workspaceDir() string { return filepath.Join(c.Dir, "workspace") }
func (c *Company) stateFile() string    { return filepath.Join(c.Dir, "STATE.md") }
func (c *Company) skillsIndex() string  { return filepath.Join(c.skillsDir(), "INDEX.md") }

func (c *Company) loadAgent(name string) (*Agent, error) {
	b, err := os.ReadFile(filepath.Join(c.agentsDir(), name+".md"))
	if err != nil {
		return nil, fmt.Errorf("agent %q not found in %s", name, c.agentsDir())
	}
	fm, body := parseFrontmatter(string(b))
	return &Agent{
		Name:     name,
		Title:    orDefault(fm["title"], name),
		Provider: orDefault(fm["provider"], "deepseek"),
		Model:    orDefault(fm["model"], "deepseek-chat"),
		Persona:  strings.TrimSpace(body),
	}, nil
}

func (c *Company) listTasks() ([]*Task, error) {
	entries, err := os.ReadDir(c.tasksDir())
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
		b, err := os.ReadFile(filepath.Join(c.tasksDir(), e.Name()))
		if err != nil {
			continue
		}
		fm, body := parseFrontmatter(string(b))
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

func (c *Company) taskPath(id string) string {
	return filepath.Join(c.tasksDir(), "task-"+id+".md")
}

func (c *Company) saveTask(t *Task) error {
	fm := map[string]string{
		"id": t.ID, "title": t.Title, "status": t.Status,
		"assignee": t.Assignee, "claimed_at": t.ClaimedAt,
	}
	order := []string{"id", "title", "status", "assignee", "claimed_at"}
	return os.WriteFile(c.taskPath(t.ID), []byte(renderFrontmatter(fm, order, t.Body)), 0o644)
}

func (c *Company) findTask(id string) (*Task, error) {
	tasks, err := c.listTasks()
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("task #%s not found", id)
}

// pickActiveTask re-derives the active task from disk (reconcile-from-reality):
// resume an in-progress task this agent owns, else claim the first open task.
func (c *Company) pickActiveTask(agent string) (*Task, error) {
	tasks, err := c.listTasks()
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		if t.Status == "in_progress" && (t.Assignee == agent || t.Assignee == "") {
			return t, nil
		}
	}
	for _, t := range tasks {
		if t.Status == "open" {
			return t, nil
		}
	}
	return nil, nil
}

// claim marks a task owned and in-progress (the overlap lock).
func (c *Company) claim(t *Task, agent string) error {
	t.Status = "in_progress"
	t.Assignee = agent
	t.ClaimedAt = nowStamp()
	return c.saveTask(t)
}

// reflectionInstruction tells the agent to end with a single fenced json block we parse.
const reflectionInstruction = "End your reply with your reflection as ONE fenced json code block and nothing after it:\n" +
	"```json\n" +
	"{\n" +
	`  "summary": "what you did this tick",` + "\n" +
	`  "state_delta": "what changed in the world (one line; goes into STATE.md)",` + "\n" +
	`  "task_status": "in_progress | blocked | done | needs_human",` + "\n" +
	`  "lessons": [{"skill": "short-kebab-name", "note": "a learning, caveat, pitfall or gotcha"}],` + "\n" +
	`  "next": "what should happen on the next tick",` + "\n" +
	`  "cadence_signal": "idle | working | blocked",` + "\n" +
	`  "hitl_question": "the question for the CEO, only when task_status is needs_human"` + "\n" +
	"}\n" +
	"```\n" +
	"Use lessons for anything worth remembering next time. Set task_status to done only when the task is fully complete and verified."

func buildSystemPrompt(a *Agent) string {
	return fmt.Sprintf(`You are %s (%s) at a company operated by mago.

%s

mago operating contract (read carefully):
- The files and the workspace are the source of truth — NOT your memory or assumptions.
- You are running ONE work tick. Read the BRIEFING in the user message before doing anything.
- Do real work in the current working directory (the workspace) using your tools.
- The workspace is for the company's PRODUCT code ONLY. Do NOT create or edit mago
  bookkeeping files (STATE.md, tasks, skills, journals) — the worker writes those from
  your reflection. Never create your own STATE.md.
- NEVER redo work the briefing/progress log shows is already done. Build on it.
- If you are blocked on a decision only the CEO (human) can make, set task_status to
  "needs_human" and put the question in "hitl_question" — do not guess.
- When you have made sensible progress for this tick, STOP and write your reflection.

%s`,
		a.Title, a.Name, a.Persona, reflectionInstruction)
}

// buildBriefing assembles the per-tick context pack the agent reads first.
func (c *Company) buildBriefing(a *Agent, t *Task) string {
	var b strings.Builder
	b.WriteString("# BRIEFING\n\n")
	b.WriteString("## Your role\n" + a.Title + "\n\n")
	b.WriteString("## Company state (STATE.md)\n" + readFileOr(c.stateFile(), "(empty)") + "\n\n")
	b.WriteString(fmt.Sprintf("## Active task #%s: %s\nstatus: %s\n\n%s\n\n", t.ID, t.Title, t.Status, t.Body))
	b.WriteString("## Skills (learnings/caveats/pitfalls from past work)\n" + c.allSkillsText() + "\n\n")
	b.WriteString("## Your recent runs\n" + c.recentJournalSummaries(a.Name, 3) + "\n\n")
	b.WriteString("## Instruction\nWork on the active task for this tick. First check the progress log and state to " +
		"see what is already done — do not repeat it. Make concrete progress, then emit your reflection JSON.\n")
	return b.String()
}

func (c *Company) allSkillsText() string {
	idx := readFileOr(c.skillsIndex(), "")
	entries, err := os.ReadDir(c.skillsDir())
	var b strings.Builder
	if idx != "" {
		b.WriteString(idx + "\n")
	}
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if bb, err := os.ReadFile(filepath.Join(c.skillsDir(), e.Name(), "SKILL.md")); err == nil {
				b.WriteString("\n--- skill: " + e.Name() + " ---\n" + string(bb) + "\n")
			}
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		return "(no skills yet)"
	}
	return b.String()
}

func (c *Company) recentJournalSummaries(agent string, n int) string {
	entries, err := os.ReadDir(filepath.Join(c.runsDir(), agent))
	if err != nil {
		return "(none yet)"
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			files = append(files, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	if len(files) > n {
		files = files[:n]
	}
	if len(files) == 0 {
		return "(none yet)"
	}
	var b strings.Builder
	for i := len(files) - 1; i >= 0; i-- {
		bb, err := os.ReadFile(filepath.Join(c.runsDir(), agent, files[i]))
		if err != nil {
			continue
		}
		var j struct {
			Ts      string `json:"ts"`
			Summary string `json:"summary"`
		}
		if json.Unmarshal(bb, &j) == nil {
			b.WriteString("- " + j.Ts + ": " + j.Summary + "\n")
		}
	}
	return b.String()
}
