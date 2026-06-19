package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writeBack is the worker doing all bookkeeping from the agent's reflection.
func (c *Company) writeBack(a *Agent, t *Task, r *Reflection, raw string) error {
	ts := nowStamp()
	c.writeJournal(a, t, r, ts)
	if oneLine(r.StateDelta) != "" {
		c.appendState(fmt.Sprintf("- %s [%s] %s", ts, a.Name, oneLine(r.StateDelta)))
	}
	for _, l := range r.Lessons {
		if oneLine(l.Skill) == "" || oneLine(l.Note) == "" {
			continue
		}
		c.appendSkill(l.Skill, l.Note, a.Name, ts)
	}
	c.applyTaskStatus(t, a, r, ts)
	return nil
}

func (c *Company) applyTaskStatus(t *Task, a *Agent, r *Reflection, ts string) {
	switch r.TaskStatus {
	case "done":
		t.Status = "done"
		t.Body += progressEntry(ts, a.Name, "DONE: "+r.Summary+" (next: "+r.Next+")")
	case "needs_human":
		t.Status = "needs_human"
		q := r.HitlQuestion
		if q == "" {
			q = r.Next
		}
		c.writeInbox(t, a, q, ts)
		t.Body += progressEntry(ts, a.Name, "NEEDS HUMAN: "+q)
	case "blocked":
		t.Status = "blocked"
		t.Body += progressEntry(ts, a.Name, "BLOCKED: "+r.Summary)
	default:
		t.Status = "in_progress"
		t.Body += progressEntry(ts, a.Name, r.Summary+" (next: "+r.Next+")")
	}
	c.saveTask(t)
}

func progressEntry(ts, who, text string) string {
	return fmt.Sprintf("\n\n### %s [%s]\n%s\n", ts, who, text)
}

func (c *Company) writeJournal(a *Agent, t *Task, r *Reflection, ts string) {
	dir := filepath.Join(c.runsDir(), a.Name)
	ensureDir(dir)
	rec := map[string]any{
		"ts": ts, "agent": a.Name, "task_id": t.ID,
		"summary": r.Summary, "state_delta": r.StateDelta,
		"task_status": r.TaskStatus, "lessons": r.Lessons,
		"next": r.Next, "cadence_signal": r.CadenceSignal,
	}
	if b, err := json.MarshalIndent(rec, "", "  "); err == nil {
		os.WriteFile(filepath.Join(dir, ts+".json"), b, 0o644)
	}
	md := fmt.Sprintf("# Run %s\n\n**Agent:** %s\n**Task:** #%s %s\n**Status:** %s\n\n## Summary\n%s\n\n## Next\n%s\n",
		ts, a.Name, t.ID, t.Title, r.TaskStatus, r.Summary, r.Next)
	os.WriteFile(filepath.Join(dir, ts+".md"), []byte(md), 0o644)
}

func (c *Company) writeRawFailure(a *Agent, t *Task, raw string) {
	dir := filepath.Join(c.runsDir(), a.Name)
	ensureDir(dir)
	os.WriteFile(filepath.Join(dir, nowStamp()+"-RAW.txt"), []byte(raw), 0o644)
}

func (c *Company) appendState(line string) {
	p := c.stateFile()
	if _, err := os.Stat(p); err != nil {
		os.WriteFile(p, []byte("# Company state\n\n## Activity log\n\n"), 0o644)
	}
	if f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		f.WriteString(line + "\n")
		f.Close()
	}
}

func (c *Company) appendSkill(skill, note, agent, ts string) {
	name := sanitize(skill)
	dir := filepath.Join(c.skillsDir(), name)
	ensureDir(dir)
	p := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(p); err != nil {
		fm := map[string]string{"name": name, "description": oneLine(truncate(note, 80))}
		body := "## Notes\n\n- " + ts + " [" + agent + "] " + note + "\n"
		os.WriteFile(p, []byte(renderFrontmatter(fm, []string{"name", "description"}, body)), 0o644)
		c.appendIndexLine(name, oneLine(truncate(note, 80)))
		return
	}
	// dedupe: skip notes already recorded verbatim in this skill
	if existing, err := os.ReadFile(p); err == nil && strings.Contains(string(existing), strings.TrimSpace(note)) {
		return
	}
	if f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		f.WriteString("- " + ts + " [" + agent + "] " + note + "\n")
		f.Close()
	}
}

func (c *Company) appendIndexLine(name, hook string) {
	ensureDir(c.skillsDir())
	p := c.skillsIndex()
	if _, err := os.Stat(p); err != nil {
		os.WriteFile(p, []byte("# Skills index\n\n"), 0o644)
	}
	if f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		f.WriteString("- " + name + " — " + hook + "\n")
		f.Close()
	}
}

func (c *Company) writeInbox(t *Task, a *Agent, q, ts string) {
	ensureDir(c.inboxDir())
	body := fmt.Sprintf("from: %s\ntask: #%s %s\nat: %s\n\nQUESTION:\n%s\n", a.Name, t.ID, t.Title, ts, q)
	os.WriteFile(filepath.Join(c.inboxDir(), "task-"+t.ID+".md"), []byte(body), 0o644)
}

func (c *Company) printRunResult(a *Agent, t *Task, r *Reflection) {
	fmt.Printf("\n--- tick complete ---\n")
	fmt.Printf("agent:    %s\n", a.Name)
	fmt.Printf("task:     #%s %s\n", t.ID, t.Title)
	fmt.Printf("status:   %s\n", r.TaskStatus)
	fmt.Printf("summary:  %s\n", oneLine(r.Summary))
	if len(r.Lessons) > 0 {
		fmt.Printf("lessons:  %d recorded\n", len(r.Lessons))
	}
	fmt.Printf("next:     %s\n", oneLine(r.Next))
	if r.TaskStatus == "needs_human" {
		q := r.HitlQuestion
		if q == "" {
			q = r.Next
		}
		fmt.Printf("\n⚠ needs human: %s\n  answer with: mago answer %s \"...\" -C %s\n", oneLine(q), t.ID, c.Dir)
	}
}
