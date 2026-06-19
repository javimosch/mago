package main

import (
	"strconv"
	"strings"
)

// Agent is a company worker definition (.mago/agents/<name>.md).
type Agent struct {
	Name     string
	Title    string
	Provider string
	Model    string
	Persona  string
}

// Task is a unit of work (tasks/task-<id>.md). The Body holds the description
// and an append-only progress log.
type Task struct {
	ID        string
	Title     string
	Status    string // open, in_progress, blocked, needs_human, done
	Assignee  string
	ClaimedAt string
	Body      string
}

// Lesson is a learning/caveat/pitfall the agent recorded, destined for skills.
type Lesson struct {
	Skill string `json:"skill"`
	Note  string `json:"note"`
}

// Reflection is the schema-forced structured output every tick must produce.
// The worker (not the agent) does all bookkeeping from it.
type Reflection struct {
	Summary       string   `json:"summary"`
	StateDelta    string   `json:"state_delta"`
	TaskStatus    string   `json:"task_status"`
	Lessons       []Lesson `json:"lessons"`
	Next          string   `json:"next"`
	CadenceSignal string   `json:"cadence_signal"`
	HitlQuestion  string   `json:"hitl_question"`
}

// reflectionSchema is passed to tau via --schema so the final answer is valid JSON.
const reflectionSchema = `{
  "type": "object",
  "properties": {
    "summary": {"type": "string", "description": "what you did this tick"},
    "state_delta": {"type": "string", "description": "what changed in the world; appended to STATE.md"},
    "task_status": {"type": "string", "enum": ["in_progress", "blocked", "done", "needs_human"]},
    "lessons": {"type": "array", "items": {"type": "object", "properties": {"skill": {"type": "string"}, "note": {"type": "string"}}, "required": ["skill", "note"]}},
    "next": {"type": "string", "description": "what should happen on the next tick"},
    "cadence_signal": {"type": "string", "enum": ["idle", "working", "blocked"]},
    "hitl_question": {"type": "string", "description": "the question for the human, only when task_status is needs_human"}
  },
  "required": ["summary", "state_delta", "task_status", "next", "cadence_signal"]
}`

// parseFrontmatter splits a "--- key: value ---" header from the markdown body.
func parseFrontmatter(content string) (map[string]string, string) {
	fm := map[string]string{}
	if !strings.HasPrefix(content, "---") {
		return fm, content
	}
	lines := strings.Split(content, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return fm, content
	}
	for i := 1; i < end; i++ {
		idx := strings.Index(lines[i], ":")
		if idx < 0 {
			continue
		}
		k := strings.TrimSpace(lines[i][:idx])
		v := strings.Trim(strings.TrimSpace(lines[i][idx+1:]), "\"")
		fm[k] = v
	}
	body := strings.TrimLeft(strings.Join(lines[end+1:], "\n"), "\n")
	return fm, body
}

// renderFrontmatter writes a frontmatter header (in the given key order) + body.
func renderFrontmatter(fm map[string]string, order []string, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range order {
		if v, ok := fm[k]; ok {
			b.WriteString(k + ": " + v + "\n")
		}
	}
	b.WriteString("---\n")
	if body != "" {
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func atoiSafe(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
func fmtID(n int) string    { return strconv.Itoa(n) }
