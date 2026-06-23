package main

import (
	"fmt"
	"os"
	"strings"
)

// backlog.go is the proactive-planning loop: on a cadence (MAGO_PROACTIVE seconds), the planner
// (Head of Product) reads the mission in STATE.md and FILES new issues to advance it — so the
// company sets its own agenda instead of waiting for the CEO to file every task. Opt-in and capped
// so it never spams: it proposes only when the active backlog is below proactiveBacklogCap, at most
// proactivePerCycle per cycle, and never duplicates open/shipped work.

const (
	proactiveBacklogCap = 3 // skip if this many tasks are already active (not done)
	proactivePerCycle   = 2 // max new issues filed per planning cycle (override: MAGO_PROACTIVE_MAX)
)

// proactiveMaxPerCycle is the per-cycle proposal cap, overridable via MAGO_PROACTIVE_MAX (e.g. 1 for
// a small smoke test).
func proactiveMaxPerCycle() int {
	if n := atoiSafe(os.Getenv("MAGO_PROACTIVE_MAX")); n > 0 {
		return n
	}
	return proactivePerCycle
}

// proposeBacklog asks the planner to file up to a few mission-advancing tasks, if the backlog is
// low. Returns the number of issues filed (0 = no work done — doesn't count against the budget).
func (c *Company) proposeBacklog() int {
	focus := c.planningFocus() // ROADMAP.md `## Now`, else STATE.md `## Mission`
	if focus == "" {
		fmt.Fprintln(os.Stderr, "[backlog] no focus set (ROADMAP.md ## Now / STATE.md ## Mission) — skipping proactive planning")
		return 0
	}
	tasks, err := c.tasks.ListTasks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[backlog] list tasks: %v\n", err)
		return 0
	}
	var openTitles []string
	active := 0
	for _, t := range tasks {
		if t.Status != "done" {
			active++
			openTitles = append(openTitles, t.Title)
		}
	}
	backlogCap := proactiveBacklogCap
	if ic := c.modeIssueCap(); ic > 0 { // operator-set per-repo open-issue cap overrides the default
		backlogCap = ic
	}
	want := backlogCap - active
	if m := proactiveMaxPerCycle(); want > m {
		want = m
	}
	if want <= 0 {
		fmt.Fprintf(os.Stderr, "[backlog] %d active task(s) >= cap %d — not proposing\n", active, backlogCap)
		return 0
	}

	planner := c.plannerAgent()
	if planner == nil {
		fmt.Fprintln(os.Stderr, "[backlog] no planner (plans: true) in roster — skipping")
		return 0
	}

	northStar := c.visionNorthStar()
	outOfScope := c.roadmapOutOfScope()
	prompt := fmt.Sprintf(`You are the Head of Product. Propose AT MOST %d concrete tasks that advance the
CURRENT FOCUS below — each self-contained and shippable as a single pull request. Output ONLY task
titles, one per line — no numbering, no prose, no duplicates of work already open or shipped.

This is the company's roadmap focus, not a backlog to fill: propose ONLY genuinely valuable work that
moves the focus forward. If there is nothing valuable to do right now within scope, output NOTHING
(empty) — do not invent filler (docs/test/cleanup busywork) just to produce titles. Never propose
anything under OUT OF SCOPE.

If — and ONLY if — the CURRENT FOCUS is substantially ACHIEVED (the shipped work already covers it and
no valuable work remains within it), output exactly one line and nothing else:
FOCUS_COMPLETE

NORTH STAR:
%s

CURRENT FOCUS (Now):
%s

OUT OF SCOPE (never propose these):
%s

ALREADY OPEN (do not duplicate):
%s

ALREADY SHIPPED (do not repeat):
%s`, want, orNone(northStar), focus, orNone(outOfScope),
		orNone(strings.Join(openTitles, "\n")), orNone(c.shippedText()))

	out, err := tauComplete(planner, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[backlog] planner failed: %v\n", err)
		return 0
	}

	// Outcome loop: the planner judged the current focus achieved. Advance Now<-Next only when the
	// backlog is also fully drained (no active work) and we're roadmap-driven — conservative, so an
	// in-flight focus is never rotated out from under work.
	if strings.Contains(strings.ToUpper(out), "FOCUS_COMPLETE") {
		switch {
		case active > 0:
			fmt.Fprintf(os.Stderr, "[backlog] focus complete but %d task(s) still active — holding\n", active)
		case isPlaceholder(c.roadmapNow()):
			break // mission-mode (no roadmap Now) — nothing to advance
		case c.advanceRoadmap():
			fmt.Fprintf(os.Stderr, "[backlog] focus complete -> advanced ROADMAP Now<-Next: %s\n", oneLine(c.roadmapNow()))
		default:
			fmt.Fprintln(os.Stderr, "[backlog] focus complete but no Next set — add ROADMAP.md ## Next to advance")
		}
		return 0
	}

	filed := 0
	for _, line := range strings.Split(out, "\n") {
		if filed >= want {
			break
		}
		title := cleanTaskTitle(line)
		if title == "" || duplicateTitle(title, openTitles) {
			continue
		}
		t, err := c.tasks.AddTask(title, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "[backlog] file %q: %v\n", title, err)
			continue
		}
		c.tasks.RecordProgress(t, planner.Name, "📋 Proposed by the Head of Product to advance the mission.")
		openTitles = append(openTitles, title)
		filed++
		fmt.Fprintf(os.Stderr, "[backlog] proposed #%s: %s\n", t.ID, title)
	}
	if filed == 0 {
		fmt.Fprintln(os.Stderr, "[backlog] planner proposed nothing new")
	}
	return filed
}

// plannerAgent loads the designated planner (frontmatter `plans: true`), or nil.
func (c *Company) plannerAgent() *Agent {
	names, err := c.loadAgentNames()
	if err != nil {
		return nil
	}
	for _, n := range names {
		if a, err := c.loadAgent(n); err == nil && a.Plans {
			applyModelOverrides(a)
			return a
		}
	}
	return nil
}

// missionText returns the ## Mission section of STATE.md, or "" if unset/placeholder.
func (c *Company) missionText() string {
	s := c.stateSections()
	if s == nil {
		return ""
	}
	m := strings.TrimSpace(s.Mission)
	if m == "" || strings.HasPrefix(m, "(") { // "(Set by the CEO. Edit me.)" placeholder
		return ""
	}
	return m
}

// setMission rewrites the `## Mission` section body of STATE.md, preserving everything else. Used
// to keep a CEO's locally-set mission from being clobbered when the worker adopts a stale remote
// STATE.md off the mago-state branch.
func (c *Company) setMission(mission string) {
	b, err := os.ReadFile(c.stateFile())
	if err != nil {
		return
	}
	lines := strings.Split(string(b), "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		out = append(out, lines[i])
		if strings.TrimSpace(lines[i]) == "## Mission" {
			out = append(out, mission, "")
			for i++; i < len(lines) && !strings.HasPrefix(lines[i], "## "); i++ { // drop old body
			}
			i-- // re-process the next "## " header on the outer loop
		}
	}
	os.WriteFile(c.stateFile(), []byte(strings.Join(out, "\n")), 0o644)
}

func (c *Company) shippedText() string {
	s := c.stateSections()
	if s == nil {
		return ""
	}
	t := strings.TrimSpace(s.Shipped)
	if strings.HasPrefix(t, "(") { // "(nothing yet)" placeholder
		return ""
	}
	return t
}

func (c *Company) stateSections() *stateSections {
	b, err := os.ReadFile(c.stateFile())
	if err != nil {
		return nil
	}
	return parseStateSections(string(b))
}

// cleanTaskTitle strips a leading bullet/ordered-list marker and surrounding quotes from a planner
// output line, leaving the bare task title (or "" if the line isn't a usable title).
func cleanTaskTitle(line string) string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "- ")
	t = strings.TrimPrefix(t, "* ")
	// ordered-list marker: leading digits then '.' or ')' then the title
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	if i > 0 && i < len(t) && (t[i] == '.' || t[i] == ')') {
		t = strings.TrimSpace(t[i+1:])
	}
	// strip surrounding markdown emphasis / quotes (planners like to **bold** titles)
	t = strings.TrimSpace(strings.Trim(t, "*\"'`"))
	if len(t) < 6 || len(t) > 160 { // skip empties, bare headers, run-on paragraphs
		return ""
	}
	return t
}

// duplicateTitle reports whether title overlaps an existing one (either contains the other).
func duplicateTitle(title string, existing []string) bool {
	lt := strings.ToLower(title)
	for _, e := range existing {
		le := strings.ToLower(strings.TrimSpace(e))
		if le != "" && (strings.Contains(le, lt) || strings.Contains(lt, le)) {
			return true
		}
	}
	return false
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}
