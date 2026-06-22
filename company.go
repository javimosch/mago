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
	// No explicit backlog repo? Adopt a single configured project repo as the task source, so
	// `mago project add <owner/repo>` + `mago serve` enumerates that repo's issues without also
	// requiring MAGO_GH_REPO. Ambiguous (multiple distinct project repos) -> stay local; the
	// operator sets MAGO_GH_REPO to pick the backlog repo.
	if c.ghRepo == "" {
		seen := map[string]bool{}
		var repos []string
		for _, r := range c.loadProjects() {
			if r != "" && !seen[r] {
				seen[r] = true
				repos = append(repos, r)
			}
		}
		if len(repos) == 1 {
			c.ghRepo = repos[0]
		}
	}
	if c.ghRepo != "" {
		c.tasks = &githubBackend{repo: c.ghRepo, taskLabel: os.Getenv("MAGO_TASK_LABEL")}
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

// projConf is a project entry. projects.json accepts either the simple string form
// (`"name": "owner/repo"`) or the object form (`"name": {"repo": "owner/repo",
// "mirror_issue": true}`) — mirror_issue (opt-in) makes mago open a tracking issue on the
// project repo that the PR closes.
type projConf struct {
	Repo        string `json:"repo"`
	MirrorIssue bool   `json:"mirror_issue"`
}

func (c *Company) loadProjectConfs() map[string]projConf {
	out := map[string]projConf{}
	raw := map[string]json.RawMessage{}
	if b, err := os.ReadFile(c.projectsConfigFile()); err == nil {
		json.Unmarshal(b, &raw)
	}
	for name, rm := range raw {
		var s string
		if json.Unmarshal(rm, &s) == nil {
			out[name] = projConf{Repo: s}
			continue
		}
		var pc projConf
		if json.Unmarshal(rm, &pc) == nil {
			out[name] = pc
		}
	}
	return out
}

// loadProjects returns name -> repo (back-compat for callers that only need the repo).
func (c *Company) loadProjects() map[string]string {
	m := map[string]string{}
	for n, pc := range c.loadProjectConfs() {
		m[n] = pc.Repo
	}
	return m
}

func (c *Company) projectRepo(name string) string { return c.loadProjectConfs()[name].Repo }
func (c *Company) projectMirror(name string) bool { return c.loadProjectConfs()[name].MirrorIssue }

// taskRepo is the GitHub repo a task's work targets: its project repo, or — when the task has no
// project — the company repo itself. The latter is the label-scoped mode (MAGO_TASK_LABEL): the
// issue lives on MAGO_GH_REPO and the agent works that same repo, so the PR closes it directly.
func (c *Company) taskRepo(t *Task) string {
	if t.Project != "" {
		return c.projectRepo(t.Project)
	}
	return c.ghRepo
}

// repos returns every GitHub repo this company touches (the company repo + project repos),
// deduped — i.e. the repos a worker should receive relayed webhooks for.
func (c *Company) repos() []string {
	seen := map[string]bool{}
	var out []string
	add := func(r string) {
		if r != "" && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	add(c.ghRepo)
	for _, r := range c.loadProjects() {
		add(r)
	}
	sort.Strings(out)
	return out
}

func (c *Company) saveProject(name, repo string, mirror bool) error {
	confs := c.loadProjectConfs()
	confs[name] = projConf{Repo: repo, MirrorIssue: mirror}
	// Write the simple string form unless mirror is on (then the object form).
	out := map[string]any{}
	for n, pc := range confs {
		if pc.MirrorIssue {
			out[n] = pc
		} else {
			out[n] = pc.Repo
		}
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return os.WriteFile(c.projectsConfigFile(), b, 0o644)
}

func (c *Company) mirrorsFile() string { return filepath.Join(c.magoDir(), "mirrors.json") }

func (c *Company) loadMirrors() map[string]int {
	m := map[string]int{}
	if b, err := os.ReadFile(c.mirrorsFile()); err == nil {
		json.Unmarshal(b, &m)
	}
	return m
}

// ensureMirrorIssue opens (once) a tracking issue on the project repo for task t and returns its
// number, persisting the task->issue mapping so resumes don't duplicate it. Returns 0 on failure.
func (c *Company) ensureMirrorIssue(t *Task, projectRepo string) int {
	m := c.loadMirrors()
	if n := m[t.ID]; n > 0 {
		return n
	}
	body := fmt.Sprintf("Tracking issue opened by mago for task **%s**.\n\n%s\n\n_A pull request will close this issue._",
		t.Title, truncate(oneLine(t.Body), 600))
	out, err := gh("-R", projectRepo, "issue", "create", "--title", truncate(oneLine(t.Title), 200), "--body", body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[mirror] create issue on %s: %v\n", projectRepo, err)
		return 0
	}
	n := atoiSafe(issueNumberFromURL(strings.TrimSpace(out)))
	if n > 0 {
		m[t.ID] = n
		if b, err := json.MarshalIndent(m, "", "  "); err == nil {
			os.WriteFile(c.mirrorsFile(), b, 0o644)
		}
	}
	return n
}

// workspaceFor resolves where a task's work happens: its project repo's workspace,
// or the default workspace when the task targets no specific project.
func (c *Company) workspaceFor(t *Task) string {
	if t != nil && t.Project != "" {
		return c.projectDir(t.Project)
	}
	return c.workspaceDir()
}
func (c *Company) stateFile() string   { return filepath.Join(c.Dir, "STATE.md") }
func (c *Company) skillsIndex() string { return filepath.Join(c.skillsDir(), "INDEX.md") }

func (c *Company) loadAgent(name string) (*Agent, error) {
	b, err := os.ReadFile(filepath.Join(c.agentsDir(), name+".md"))
	if err != nil {
		return nil, fmt.Errorf("agent %q not found in %s", name, c.agentsDir())
	}
	fm, body := parseFrontmatter(string(b))
	return &Agent{
		Name:       name,
		Title:      orDefault(fm["title"], name),
		Provider:   orDefault(fm["provider"], "deepseek"),
		Model:      orDefault(fm["model"], "deepseek-chat"),
		Reviews:    fm["reviews"] == "true",
		Plans:      fm["plans"] == "true",
		Implements: fm["implements"] == "true",
		Persona:    strings.TrimSpace(body),
	}, nil
}

func isPlannerRole(a *Agent) bool { return a.Plans }

// clarifyInstructions guides the planner during the mago:clarify phase: produce a plan + open
// questions and hand back to the human, iterating until they add mago:go. No code, no PR.
func clarifyInstructions() string {
	return "The CEO opened this with `mago:clarify` — it needs a planning pass BEFORE any code.\n" +
		"- Do NOT modify any repo, write code, or open a PR this tick.\n" +
		"- Read the issue and the progress log above (your prior plan + the CEO's answers).\n" +
		"- Produce a concise PLAN (approach + key steps) and a short numbered list of OPEN QUESTIONS the CEO must answer.\n" +
		"- Set task_status to \"needs_human\" and put the plan + questions in hitl_question.\n" +
		"- Each round: fold in the CEO's latest answers, tighten the plan, ask only what's still unresolved.\n" +
		"- When the plan is solid, still set needs_human, present the FINAL plan, and tell the CEO to add the " +
		"`mago:go` label to start implementation."
}

// reflectionInstruction tells the agent to end with a single fenced json block we parse.
const reflectionInstruction = "End your reply with your reflection as ONE fenced json code block and nothing after it:\n" +
	"```json\n" +
	"{\n" +
	`  "summary": "what you did this tick",` + "\n" +
	`  "state_delta": "what changed in the world (one line; goes into STATE.md)",` + "\n" +
	`  "task_status": "in_progress | blocked | done | needs_human | reassign | already_done",` + "\n" +
	`  "lessons": [{"skill": "short-kebab-name", "note": "a learning, caveat, pitfall or gotcha"}],` + "\n" +
	`  "next": "what should happen on the next tick",` + "\n" +
	`  "cadence_signal": "idle | working | blocked",` + "\n" +
	`  "hitl_question": "the question for the CEO, only when task_status is needs_human"` + "\n" +
	"}\n" +
	"```\n" +
	"Use lessons for anything worth remembering next time. Set task_status to done only when the task is fully complete and verified. " +
	"Set task_status to reassign if this task does not fit your role — it will be handed back and routed to someone else. " +
	"Set task_status to already_done if the deliverable already exists — do NOT redo it or open a duplicate; say what already covers it."

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

// openPRsText lists the project repo's open PRs so a run is aware of in-flight work
// (and can mark already_done instead of duplicating it).
func (c *Company) openPRsText(repo string) string {
	out, err := gh("-R", repo, "pr", "list", "--state", "open", "--json", "number,title,headRefName", "--limit", "30")
	if err != nil {
		return "(could not list)"
	}
	var prs []struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		HeadRefName string `json:"headRefName"`
	}
	if json.Unmarshal([]byte(out), &prs) != nil || len(prs) == 0 {
		return "(none open)"
	}
	var sb strings.Builder
	for _, p := range prs {
		sb.WriteString(fmt.Sprintf("- #%d [%s] %s\n", p.Number, p.HeadRefName, p.Title))
	}
	return sb.String()
}

// projectRepoInstructions gives ROLE-APPROPRIATE git/gh steps: implementers open a PR
// and must not merge it; reviewers review and auto-merge. Both work inside the clone.
func projectRepoInstructions(a *Agent, repo, taskID string, mirror int) string {
	header := fmt.Sprintf("This workspace is a git clone of `%s`. Work ONLY inside this directory — "+
		"do not touch other repositories or paths on the machine.\n\n", repo)
	if isReviewerRole(a) {
		return header + "You are REVIEWING. Do NOT write or modify code, and do NOT open PRs.\n" +
			"- List open mago PRs: `gh pr list --head mago/`; inspect with `gh pr diff <n>`.\n" +
			"- Post your review as a COMMENT: `gh pr comment <n> --body \"...\"` (do NOT use `gh pr review`).\n" +
			"- If the code meets the bar, merge it: `gh pr merge <n> --squash --delete-branch`.\n" +
			"- If it does not, comment what must change and do NOT merge.\n" +
			"- If there is NO open PR to review, do nothing and say so in your summary.\n" +
			"- If this task is NOT about reviewing/merging a PR (e.g. it asks you to implement or write\n" +
			"  something), set task_status to \"reassign\" so it goes to the right role — do not attempt it."
	}
	ref := "mago task #" + taskID
	if mirror > 0 {
		ref = fmt.Sprintf("Closes #%d  _(mago task #%s)_", mirror, taskID)
	}
	return header + fmt.Sprintf("You are IMPLEMENTING. Do NOT review or merge anything.\n"+
		"- You are ALREADY on branch `mago/task-%s`, freshly based on the repo's latest default branch. Work here.\n"+
		"- FIRST: if the Open PRs list above already covers this task, or the specific deliverable already exists, "+
		"do NOT duplicate it — set task_status to already_done and say what covers it.\n"+
		"- Make your change with a descriptive commit message, then push: `git push -u origin mago/task-%s`.\n"+
		"- Open a PR if none exists, with a clear title AND a real description body — never an empty one. "+
		"Write the body to a file and pass it, so it can be multi-line markdown:\n"+
		"  `gh pr create --head mago/task-%s --title \"<concise summary>\" --body-file /tmp/pr_body.md` (write the body OUTSIDE the repo so it isn't committed)\n"+
		"  The body must cover: what changed, why, and how it was verified; end with a line: `%s`. "+
		"Do NOT use `--fill` (it leaves the body empty when the commit has no body). If a PR already exists, just push more commits.\n"+
		"- STOP after opening the PR. Do NOT merge, approve, or review it — that is the reviewer's job.\n"+
		"- Put the PR URL in your summary.",
		taskID, taskID, taskID, ref)
}

// buildBriefing assembles the per-tick context pack the agent reads first.
func (c *Company) buildBriefing(a *Agent, t *Task) string {
	var b strings.Builder
	b.WriteString("# BRIEFING\n\n")
	b.WriteString("## Your role\n" + a.Title + "\n\n")
	b.WriteString("## Company state (STATE.md)\n" + readFileOr(c.stateFile(), "(empty)") + "\n\n")
	b.WriteString(fmt.Sprintf("## Active task #%s: %s\nstatus: %s\n\n%s\n\n", t.ID, t.Title, t.Status, t.Body))
	clarifyMode := t.Clarify && !t.Go
	if clarifyMode {
		b.WriteString("## CLARIFICATION PHASE (do NOT implement)\n" + clarifyInstructions() + "\n\n")
	} else if repo := c.taskRepo(t); repo != "" {
		// Which issue the PR should close (0 = none):
		//   - project + mirror_issue (B): a tracking issue opened on the project repo, once.
		//   - working the company repo directly (C, label-scoped): the task's own issue.
		mirror := 0
		if !isReviewerRole(a) {
			switch {
			case t.Project != "" && c.projectMirror(t.Project):
				mirror = c.ensureMirrorIssue(t, repo)
			case repo == c.ghRepo:
				mirror = atoiSafe(t.ID)
			}
		}
		b.WriteString("## Open PRs in " + repo + " (do not duplicate in-flight work)\n" + c.openPRsText(repo) + "\n\n")
		b.WriteString("## Project repo\n" + projectRepoInstructions(a, repo, t.ID, mirror) + "\n\n")
	}
	b.WriteString("## Skills (learnings/caveats/pitfalls from past work)\n" + c.selectSkillsText(a, t) + "\n\n")
	b.WriteString("## Your recent runs\n" + c.recentJournalSummaries(a.Name, 3) + "\n\n")
	if clarifyMode {
		b.WriteString("## Instruction\nFollow the CLARIFICATION PHASE rules above: plan + open questions, then needs_human. Do NOT write code or open a PR this tick.\n")
	} else {
		b.WriteString("## Instruction\nWork on the active task for this tick. First check the progress log and state to " +
			"see what is already done — do not repeat it. Make concrete progress, then emit your reflection JSON.\n")
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
