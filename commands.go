package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// parseCompanyDir extracts the -C <dir> flag (default cwd or $MAGO_COMPANY) and
// returns the remaining positional args.
func parseCompanyDir(args []string) (string, []string) {
	dir := "."
	if d := os.Getenv("MAGO_COMPANY"); d != "" {
		dir = d
	}
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "-C" && i+1 < len(args) {
			dir = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	return dir, rest
}

func cmdInit(args []string) error {
	dir := "."
	for _, a := range args {
		if a != "" && !strings.HasPrefix(a, "-") {
			dir = a
			break
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, d := range []string{
		filepath.Join(abs, ".mago", "agents"),
		filepath.Join(abs, ".mago", "skills"),
		filepath.Join(abs, ".mago", "runs"),
		filepath.Join(abs, ".mago", "inbox"),
		filepath.Join(abs, "tasks"),
		filepath.Join(abs, "workspace"),
	} {
		if err := ensureDir(d); err != nil {
			return err
		}
	}
	name := filepath.Base(abs)
	writeIfMissing(filepath.Join(abs, ".mago", "agents", "cto.md"), defaultAgentMD)
	writeIfMissing(filepath.Join(abs, "STATE.md"), fmt.Sprintf(stateTemplate, name))
	writeIfMissing(filepath.Join(abs, ".mago", "skills", "INDEX.md"), "# Skills index\n\n")

	fmt.Printf("initialized mago company %q at %s\n", name, abs)
	fmt.Printf("  agent: cto\n")
	fmt.Printf("  next:  mago task add \"<title>\" -C %s\n", abs)
	return nil
}

func cmdTask(args []string) error {
	dir, rest := parseCompanyDir(args)
	if len(rest) < 2 || rest[0] != "add" {
		return fmt.Errorf("usage: mago task add \"<title>\" [-C dir]")
	}
	title := strings.Join(rest[1:], " ")
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	tasks, err := comp.listTasks()
	if err != nil {
		return err
	}
	maxID := 0
	for _, t := range tasks {
		if n := atoiSafe(t.ID); n > maxID {
			maxID = n
		}
	}
	t := &Task{
		ID:     fmtID(maxID + 1),
		Title:  title,
		Status: "open",
		Body:   "## Description\n" + title + "\n\n## Progress log\n",
	}
	if err := comp.saveTask(t); err != nil {
		return err
	}
	fmt.Printf("created task #%s: %s\n", t.ID, t.Title)
	return nil
}

func cmdStatus(args []string) error {
	dir, _ := parseCompanyDir(args)
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	fmt.Printf("# company: %s\n\n", comp.Name)
	fmt.Println(readFileOr(comp.stateFile(), "(no STATE.md)"))

	fmt.Println("\n## Tasks")
	tasks, _ := comp.listTasks()
	if len(tasks) == 0 {
		fmt.Println("  (none)")
	}
	for _, t := range tasks {
		as := orDefault(t.Assignee, "-")
		fmt.Printf("  #%s [%s] %s (assignee: %s)\n", t.ID, t.Status, t.Title, as)
	}

	entries, _ := os.ReadDir(comp.inboxDir())
	var pending []os.DirEntry
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			pending = append(pending, e)
		}
	}
	if len(pending) > 0 {
		fmt.Println("\n## Pending human input (HITL)")
		for _, e := range pending {
			b, _ := os.ReadFile(filepath.Join(comp.inboxDir(), e.Name()))
			fmt.Printf("%s\n", string(b))
		}
	}
	return nil
}

func cmdAnswer(args []string) error {
	dir, rest := parseCompanyDir(args)
	if len(rest) < 2 {
		return fmt.Errorf("usage: mago answer <task-id> \"<text>\" [-C dir]")
	}
	id := rest[0]
	text := strings.Join(rest[1:], " ")
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	t, err := comp.findTask(id)
	if err != nil {
		return err
	}
	t.Body += progressEntry(nowStamp(), "ceo", "HUMAN ANSWER: "+text)
	t.Status = "in_progress"
	if err := comp.saveTask(t); err != nil {
		return err
	}
	os.Remove(filepath.Join(comp.inboxDir(), "task-"+id+".md"))
	fmt.Printf("answer recorded on task #%s; it resumes on the next `mago run`\n", id)
	return nil
}

func writeIfMissing(path, content string) {
	if _, err := os.Stat(path); err != nil {
		os.WriteFile(path, []byte(content), 0o644)
	}
}

const defaultAgentMD = `---
name: cto
title: Chief Technology Officer
provider: deepseek
model: deepseek-chat
---
You are the CTO of this company. You own engineering across the company's work.
You implement tasks in the workspace with clean, simple, working code and tests.
You value correctness and small, verifiable steps. Before doing anything, read the
briefing and the task's progress log; never redo work that is already done. When you
hit a decision only the CEO can make, ask via needs_human rather than guessing.
`

const stateTemplate = `# %s — company state

## Mission
(Set by the CEO. Edit me.)

## Shipped
(nothing yet)

## In flight
(nothing yet)

## Decisions
(none yet)

## Activity log
`
