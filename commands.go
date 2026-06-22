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
		filepath.Join(abs, "projects"),
	} {
		if err := ensureDir(d); err != nil {
			return err
		}
	}
	name := filepath.Base(abs)
	agentsDir := filepath.Join(abs, ".mago", "agents")
	for fname, content := range starterTeam {
		writeIfMissing(filepath.Join(agentsDir, fname), content)
	}
	// Backfill role flags on EXISTING companies (writeIfMissing won't touch their agent files):
	// companies scaffolded before these flags existed need them to use review/clarify routing.
	backfillAgentFlag(filepath.Join(agentsDir, "head-of-product.md"), "plans", "true")
	backfillAgentFlag(filepath.Join(agentsDir, "head-of-org-engineering.md"), "reviews", "true")
	backfillAgentFlag(filepath.Join(agentsDir, "cto.md"), "implements", "true")
	writeIfMissing(filepath.Join(abs, "STATE.md"), fmt.Sprintf(stateTemplate, name))
	writeIfMissing(filepath.Join(abs, ".mago", "skills", "INDEX.md"), "# Skills index\n\n")

	fmt.Printf("initialized mago company %q at %s\n", name, abs)
	fmt.Printf("  team: cto, cmo, head-of-product, head-of-org-engineering (you are the CEO)\n")
	fmt.Printf("  next: mago task add \"<title>\" -C %s\n", abs)
	return nil
}

func cmdTask(args []string) error {
	dir, rest := parseCompanyDir(args)
	if len(rest) < 2 || rest[0] != "add" {
		return fmt.Errorf("usage: mago task add \"<title>\" [-C dir]")
	}
	project := ""
	var words []string
	for i := 1; i < len(rest); i++ {
		if rest[i] == "--project" && i+1 < len(rest) {
			project = rest[i+1]
			i++
			continue
		}
		words = append(words, rest[i])
	}
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	t, err := comp.tasks.AddTask(strings.Join(words, " "), project)
	if err != nil {
		return err
	}
	fmt.Printf("created task #%s: %s%s\n", t.ID, t.Title, ifStr(project != "", " [project: "+project+"]", ""))
	return nil
}

func cmdProject(args []string) error {
	dir, rest := parseCompanyDir(args)
	repo := ""
	mirror := false
	var pos []string
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--repo" && i+1 < len(rest) {
			repo = rest[i+1]
			i++
			continue
		}
		if rest[i] == "--mirror" {
			mirror = true
			continue
		}
		pos = append(pos, rest[i])
	}
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	switch {
	case len(pos) >= 1 && pos[0] == "list":
		confs := comp.loadProjectConfs()
		if len(confs) == 0 {
			fmt.Println("(no projects — add one with `mago project add <name> --repo owner/repo`)")
			return nil
		}
		for name, pc := range confs {
			fmt.Printf("  %s -> %s%s\n", name, orDefault(pc.Repo, "(no repo)"), ifStr(pc.MirrorIssue, "  [mirror-issue]", ""))
		}
		return nil
	case len(pos) >= 2 && pos[0] == "add":
		name := pos[1]
		// Accept `mago project add owner/repo` as shorthand: infer repo + project name.
		if repo == "" && strings.Contains(name, "/") {
			repo = name
			name = name[strings.LastIndex(name, "/")+1:]
		}
		if err := ensureDir(comp.projectDir(name)); err != nil {
			return err
		}
		if repo != "" {
			if err := comp.saveProject(name, repo, mirror); err != nil {
				return err
			}
		}
		fmt.Printf("project %q ready%s%s\n", name, ifStr(repo != "", " -> "+repo, " (no repo set — pass --repo owner/repo)"), ifStr(mirror, " [mirror-issue on]", ""))
		return nil
	}
	return fmt.Errorf("usage:\n  mago project add <name> --repo owner/repo [--mirror] [-C dir]\n  mago project add owner/repo [-C dir]\n  mago project list [-C dir]")
}

func ifStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func cmdStatus(args []string) error {
	dir, _ := parseCompanyDir(args)
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	fmt.Printf("# company: %s\n\n", comp.Name)
	fmt.Println(readFileOr(comp.stateFile(), "(no STATE.md)"))

	if projects := comp.loadProjects(); len(projects) > 0 {
		fmt.Println("\n## Projects")
		for name, r := range projects {
			fmt.Printf("  %s -> %s\n", name, orDefault(r, "(no repo)"))
		}
	}

	fmt.Println("\n## Tasks")
	tasks, err := comp.tasks.ListTasks()
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Println("  (none)")
	}
	for _, t := range tasks {
		fmt.Printf("  #%s [%s] %s (assignee: %s)\n", t.ID, t.Status, t.Title, orDefault(t.Assignee, "-"))
	}

	if pending, _ := comp.tasks.PendingHITL(); len(pending) > 0 {
		fmt.Println("\n## Pending human input (HITL)")
		for _, p := range pending {
			fmt.Println(p)
		}
	}
	return nil
}

func cmdAnswer(args []string) error {
	dir, rest := parseCompanyDir(args)
	if len(rest) < 2 {
		return fmt.Errorf("usage: mago answer <task-id> \"<text>\" [-C dir]")
	}
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	if err := comp.tasks.AnswerHITL(rest[0], strings.Join(rest[1:], " ")); err != nil {
		return err
	}
	fmt.Printf("answer recorded on task #%s; it resumes on the next `mago run`\n", rest[0])
	return nil
}

func writeIfMissing(path, content string) {
	if _, err := os.Stat(path); err != nil {
		os.WriteFile(path, []byte(content), 0o644)
	}
}

// backfillAgentFlag ensures an existing agent file has a frontmatter flag (e.g. plans: true),
// adding it if missing. No-op if the file is absent or the flag is already set.
func backfillAgentFlag(path, key, val string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	fm, body := parseFrontmatter(string(b))
	if fm[key] == val {
		return
	}
	fm[key] = val
	order := []string{"name", "title", "provider", "model", "reviews", "plans", "implements"}
	os.WriteFile(path, []byte(renderFrontmatter(fm, order, body)), 0o644)
}

// starterTeam is the executive team seeded by `mago init`. The CEO is the human.
var starterTeam = map[string]string{
	"cto.md":                     personaCTO,
	"cmo.md":                     personaCMO,
	"head-of-product.md":         personaHeadProduct,
	"head-of-org-engineering.md": personaHeadOrgEng,
}

const personaCTO = `---
name: cto
title: Chief Technology Officer
provider: opencode-go
model: deepseek-v4-flash
implements: true
---
You are the CTO. You own engineering across the company's project repos. You pick up
engineering tasks and implement them as clean, well-tested code shipped as pull requests.
You implement; you do NOT merge your own work — the Head of Org Engineering reviews and
merges. Read the briefing and progress log first; never redo finished work. When only the
CEO can decide something, ask via needs_human. Record gotchas as lessons.
`

const personaCMO = `---
name: cmo
title: Chief Marketing Officer
provider: opencode-go
model: deepseek-v4-flash
---
You are the CMO. You own marketing and growth: positioning, READMEs and docs, landing
copy, release notes, and announcements. You write clear, compelling copy. You do not
change core application code. Record useful messaging and lessons as skills.
`

const personaHeadProduct = `---
name: head-of-product
title: Head of Product
provider: opencode-go
model: deepseek-v4-flash
plans: true
---
You are the Head of Product. You turn the CEO's intent into concrete specs and
prioritized, well-scoped tasks with clear acceptance criteria. You define WHAT to build
and why — not how to implement it. You run the clarification phase: when an issue is opened
with mago:clarify, draft the plan and the open questions for the CEO, and iterate until they
approve with mago:go. When something is ambiguous and only the CEO can decide, ask via
needs_human. Record product decisions as lessons.
`

const personaHeadOrgEng = `---
name: head-of-org-engineering
title: Head of Org Engineering
reviews: true
provider: opencode-go
model: deepseek-v4-flash
---
You are the Head of Org Engineering. You safeguard the company's quality and engineering
process. You REVIEW open mago pull requests and merge the ones that are correct, safe, and
properly scoped; you do NOT implement features yourself. Request changes only for real
blocking defects (bugs, invalid syntax, out-of-scope or destructive edits, leaked secrets)
— not for missing tests, docs, or polish. You also keep the company's skills healthy.
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
