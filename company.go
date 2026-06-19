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
// Task/HITL operations are delegated to a TaskBackend (local files or GitHub).
type Company struct {
	Dir    string
	Name   string
	ghRepo string
	tasks  TaskBackend
}

func loadCompany(dir string) (*Company, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(abs, ".mago")); err != nil {
		return nil, fmt.Errorf("not a mago company (no .mago/) at %s — run `mago init` first", abs)
	}
	c := &Company{Dir: abs, Name: filepath.Base(abs), ghRepo: os.Getenv("MAGO_GH_REPO")}
	if c.ghRepo != "" {
		c.tasks = &githubBackend{repo: c.ghRepo}
	} else {
		c.tasks = &localBackend{c: c}
	}
	return c, nil
}

func (c *Company) magoDir() string      { return filepath.Join(c.Dir, ".mago") }
func (c *Company) agentsDir() string    { return filepath.Join(c.magoDir(), "agents") }
func (c *Company) skillsDir() string    { return filepath.Join(c.magoDir(), "skills") }
func (c *Company) runsDir() string      { return filepath.Join(c.magoDir(), "runs") }
func (c *Company) inboxDir() string     { return filepath.Join(c.magoDir(), "inbox") }
func (c *Company) tasksDir() string     { return filepath.Join(c.Dir, "tasks") }
func (c *Company) workspaceDir() string { return filepath.Join(c.Dir, "workspace") }
func (c *Company) projectsDir() string  { return filepath.Join(c.Dir, "projects") }
func (c *Company) projectDir(name string) string {
	return filepath.Join(c.projectsDir(), name)
}
func (c *Company) projectsConfigFile() string { return filepath.Join(c.magoDir(), "projects.json") }

// loadProjects returns the project-name -> GitHub repo (owner/repo) mapping.
func (c *Company) loadProjects() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(c.projectsConfigFile()); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

func (c *Company) projectRepo(name string) string { return c.loadProjects()[name] }

func (c *Company) saveProject(name, repo string) error {
	m := c.loadProjects()
	m[name] = repo
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(c.projectsConfigFile(), b, 0o644)
}

// workspaceFor resolves where a task's work happens: its project repo's workspace,
// or the default workspace when the task targets no specific project.
func (c *Company) workspaceFor(t *Task) string {
	if t != nil && t.Project != "" {
		return c.projectDir(t.Project)
	}
	return c.workspaceDir()
}
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
		Reviews:  fm["reviews"] == "true",
		Persona:  strings.TrimSpace(body),
	}, nil
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
- Stay strictly within your ROLE and the active task — never do another role's job
  (an implementer never merges or reviews; a reviewer never writes features or opens PRs).
- Report ONLY what you actually did. Never claim code, PRs, merges, or passing tests you
  did not really produce; if something failed or you skipped it, say so plainly.
- When you have made sensible progress for this tick, STOP and write your reflection.

%s`,
		a.Title, a.Name, a.Persona, reflectionInstruction)
}

func isReviewerRole(a *Agent) bool {
	return a.Reviews || strings.Contains(strings.ToLower(a.Name+" "+a.Title), "review")
}

// projectRepoInstructions gives ROLE-APPROPRIATE git/gh steps: implementers open a PR
// and must not merge it; reviewers review and auto-merge. Both work inside the clone.
func projectRepoInstructions(a *Agent, repo, taskID string) string {
	header := fmt.Sprintf("This workspace is a git clone of `%s`. Work ONLY inside this directory — "+
		"do not touch other repositories or paths on the machine.\n\n", repo)
	if isReviewerRole(a) {
		return header + "You are REVIEWING. Do NOT write or modify code, and do NOT open PRs.\n" +
			"- List open mago PRs: `gh pr list --head mago/`; inspect with `gh pr diff <n>`.\n" +
			"- Post your review as a COMMENT: `gh pr comment <n> --body \"...\"` (do NOT use `gh pr review`).\n" +
			"- If the code meets the bar, merge it: `gh pr merge <n> --squash --delete-branch`.\n" +
			"- If it does not, comment what must change and do NOT merge.\n" +
			"- If there is NO open PR to review, do nothing and say so in your summary."
	}
	return header + fmt.Sprintf("You are IMPLEMENTING. Do NOT review or merge anything.\n"+
		"- Use a branch `mago/task-%s` (create it, or check it out if it exists).\n"+
		"- Commit your work, then push: `git push -u origin mago/task-%s`.\n"+
		"- Open a PR if none exists: `gh pr create --fill --head mago/task-%s` (otherwise push more commits).\n"+
		"- STOP after opening the PR. Do NOT merge, approve, or review it — that is the reviewer's job.\n"+
		"- Put the PR URL in your summary.",
		taskID, taskID, taskID)
}

// buildBriefing assembles the per-tick context pack the agent reads first.
func (c *Company) buildBriefing(a *Agent, t *Task) string {
	var b strings.Builder
	b.WriteString("# BRIEFING\n\n")
	b.WriteString("## Your role\n" + a.Title + "\n\n")
	b.WriteString("## Company state (STATE.md)\n" + readFileOr(c.stateFile(), "(empty)") + "\n\n")
	b.WriteString(fmt.Sprintf("## Active task #%s: %s\nstatus: %s\n\n%s\n\n", t.ID, t.Title, t.Status, t.Body))
	if t.Project != "" {
		if repo := c.projectRepo(t.Project); repo != "" {
			b.WriteString("## Project repo\n" + projectRepoInstructions(a, repo, t.ID) + "\n\n")
		}
	}
	b.WriteString("## Skills (learnings/caveats/pitfalls from past work)\n" + c.selectSkillsText(a, t) + "\n\n")
	b.WriteString("## Your recent runs\n" + c.recentJournalSummaries(a.Name, 3) + "\n\n")
	b.WriteString("## Instruction\nWork on the active task for this tick. First check the progress log and state to " +
		"see what is already done — do not repeat it. Make concrete progress, then emit your reflection JSON.\n")
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
