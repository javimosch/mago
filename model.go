package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Agent is a company worker definition (.mago/agents/<name>.md).
type Agent struct {
	Name       string
	Title      string
	Provider   string
	Model      string
	Reviews    bool // designated PR reviewer/merger (frontmatter `reviews: true`)
	Plans      bool // designated planner for the clarification phase (frontmatter `plans: true`)
	Implements bool // designated implementer: owns code tasks by default (frontmatter `implements: true`)
	Persona    string
}

// Task is a unit of work (tasks/task-<id>.md). The Body holds the description
// and an append-only progress log.
type Task struct {
	ID        string
	Title     string
	Status    string // open, in_progress, blocked, needs_human, done
	Assignee  string
	Project   string // which project repo/workspace this task targets ("" = default)
	ClaimedAt string
	Body      string
	Clarify   bool // issue carries mago:clarify — run the planning phase before implementing
	Go        bool // issue carries mago:go — the human approved; implement now
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

// knownAgentKeys is the canonical set of frontmatter keys for agent definition files.
var knownAgentKeys = map[string]bool{
	"name": true, "title": true, "provider": true, "model": true,
	"reviews": true, "plans": true, "implements": true,
}

// validateAgentFrontmatter returns a clear, actionable error for unknown or invalid
// configuration keys in an agent frontmatter block so typos surface immediately
// instead of silently taking no effect.
func validateAgentFrontmatter(name string, fm map[string]string) error {
	for k := range fm {
		if !knownAgentKeys[k] {
			return fmt.Errorf("agent %q: unknown frontmatter key %q — valid keys: name, title, provider, model, reviews, plans, implements\n  hint: check for typos or remove the key", name, k)
		}
	}
	for _, boolKey := range []string{"reviews", "plans", "implements"} {
		if v := fm[boolKey]; v != "" && v != "true" && v != "false" {
			return fmt.Errorf("agent %q: key %q must be \"true\" or \"false\", got %q — remove the key or correct the value", name, boolKey, v)
		}
	}
	return nil
}

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
