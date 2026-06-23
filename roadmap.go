package main

import (
	"path/filepath"
	"strings"
)

// roadmap.go makes the company's intent a first-class input the agents perceive — so autonomous work
// advances the PRODUCT instead of producing plausible maintenance chores. Two artifacts, distinct
// lifecycles (mirrors AM's VISION-vs-objectives split):
//
//   VISION.md  — STABLE: north star, product, hard constraints / no-touch. Agents read it; never rewrite.
//   ROADMAP.md — STEERABLE: Now / Next / Later + Out of scope. The CEO edits it to steer; `Now` is the
//                focus the planner proposes against and the reviewer gates intent against.
//
// They feed three decision points: planning (propose only Now-advancing work), implementation (carry
// the focus + no-touch into the brief), and review (reject out-of-scope / no-touch). When ROADMAP.md
// is absent the focus falls back to STATE.md `## Mission`, so existing companies keep working.

func (c *Company) visionFile() string  { return filepath.Join(c.Dir, "VISION.md") }
func (c *Company) roadmapFile() string { return filepath.Join(c.Dir, "ROADMAP.md") }

func (c *Company) visionText() string { return readFileOr(c.visionFile(), "") }
func (c *Company) roadmapRaw() string { return readFileOr(c.roadmapFile(), "") }

// mdSection returns the trimmed body under a `## <header>` (case-insensitive) up to the next `## `.
func mdSection(md, header string) string {
	var out []string
	in := false
	for _, ln := range strings.Split(md, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "## ") {
			if in {
				break
			}
			if strings.EqualFold(strings.TrimSpace(t[3:]), header) {
				in = true
			}
			continue
		}
		if in {
			out = append(out, ln)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// isPlaceholder reports whether a section body is empty or still the scaffolded "(…)" placeholder.
func isPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	return s == "" || strings.HasPrefix(s, "(")
}

func (c *Company) visionNorthStar() string   { return mdSection(c.visionText(), "North star") }
func (c *Company) visionConstraints() string { return mdSection(c.visionText(), "Constraints") }
func (c *Company) roadmapNow() string        { return mdSection(c.roadmapRaw(), "Now") }
func (c *Company) roadmapNext() string       { return mdSection(c.roadmapRaw(), "Next") }
func (c *Company) roadmapOutOfScope() string { return mdSection(c.roadmapRaw(), "Out of scope") }

// planningFocus is what the planner proposes against: the roadmap's `Now`, else (back-compat) the
// STATE.md `## Mission`. Empty when neither is set (proactive planning then stays idle).
func (c *Company) planningFocus() string {
	if now := c.roadmapNow(); !isPlaceholder(now) {
		return now
	}
	return c.missionText()
}

// directionContext is the compact intent pack injected into the implementer brief and the reviewer
// prompt: north star + current focus + no-touch constraints + out-of-scope. Returns "" when nothing
// is set, so callers can skip the section entirely.
func (c *Company) directionContext() string {
	var b strings.Builder
	add := func(label, body string) {
		if !isPlaceholder(body) {
			b.WriteString(label + ":\n" + strings.TrimSpace(body) + "\n\n")
		}
	}
	add("North star", c.visionNorthStar())
	add("Current focus (Now)", c.roadmapNow())
	add("Constraints / no-touch", c.visionConstraints())
	add("Out of scope — never work or propose these", c.roadmapOutOfScope())
	return strings.TrimSpace(b.String())
}

const visionTemplate = `# %s — VISION

> The north star: what this company exists to build, and the limits it must respect.
> STABLE — the agents read this for intent; they do not rewrite it. (README = how to run it.)

## North star
(One or two sentences: the outcome this company is driving toward. Set by the CEO.)

## Product
(What we are building and for whom — the positioning.)

## Constraints
(Hard rules the agents must NEVER violate: locked architecture, tech constraints, no-touch areas.)
`

const roadmapTemplate = `# %s — ROADMAP

> The steerable plan. The CEO edits this to set direction; mago advances it as work ships.
> ` + "`Now`" + ` is the current focus: the planner proposes against it, the reviewer gates intent against it.

## Now
(The current focus theme — concrete outcomes to pursue NOW. Agents propose & build against THIS.)

## Next
(What comes after Now. Not yet active.)

## Later
(Parked / someday.)

## Out of scope
(Explicitly NOT to be worked on — agents must decline and never propose these.)
`
