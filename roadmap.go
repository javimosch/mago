package main

import (
	"os"
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

// --- outcome loop: auto-advance Now -> Next when the focus is achieved ---

// mdBlock is one `## <Header>` section with its raw body (for order-preserving rewrites of ROADMAP.md).
type mdBlock struct {
	Header string
	Body   string
}

// splitBlocks parses markdown into the preamble (everything before the first `## `) and ordered
// `## ` sections, preserving any sections we don't manage so a rewrite never drops the CEO's content.
func splitBlocks(md string) (string, []mdBlock) {
	var preamble []string
	var blocks []mdBlock
	cur := -1
	for _, ln := range strings.Split(md, "\n") {
		if t := strings.TrimSpace(ln); strings.HasPrefix(t, "## ") {
			blocks = append(blocks, mdBlock{Header: strings.TrimSpace(t[3:])})
			cur = len(blocks) - 1
			continue
		}
		if cur < 0 {
			preamble = append(preamble, ln)
		} else {
			blocks[cur].Body += ln + "\n"
		}
	}
	return strings.TrimRight(strings.Join(preamble, "\n"), "\n"), blocks
}

func renderBlocks(preamble string, blocks []mdBlock) string {
	var b strings.Builder
	if preamble != "" {
		b.WriteString(preamble + "\n")
	}
	for _, bl := range blocks {
		b.WriteString("\n## " + bl.Header + "\n")
		if body := strings.Trim(bl.Body, "\n"); body != "" {
			b.WriteString(body + "\n")
		}
	}
	return b.String()
}

// advanceRoadmap rotates the roadmap when the current focus is done: archive `Now` into `## Done`
// (newest first, one-lined + timestamped), then Now <- Next, Next <- Later, Later <- (none yet).
// It only advances when there is a real `Next` to promote; returns whether it advanced. Other
// sections (Out of scope, North-star prose, custom) are preserved in place.
func (c *Company) advanceRoadmap() bool {
	raw := c.roadmapRaw()
	if raw == "" {
		return false
	}
	pre, blocks := splitBlocks(raw)
	idx := func(name string) int {
		for i, b := range blocks {
			if strings.EqualFold(b.Header, name) {
				return i
			}
		}
		return -1
	}
	ni, xi, li := idx("Now"), idx("Next"), idx("Later")
	if ni < 0 || xi < 0 {
		return false
	}
	oldNow := strings.TrimSpace(blocks[ni].Body)
	nextBody := strings.TrimSpace(blocks[xi].Body)
	if isPlaceholder(nextBody) {
		return false // nothing to advance to — hold and wait for the CEO to set Next
	}
	blocks[ni].Body = "\n" + nextBody + "\n" // Now <- Next
	if li >= 0 {
		if laterBody := strings.TrimSpace(blocks[li].Body); !isPlaceholder(laterBody) {
			blocks[xi].Body = "\n" + laterBody + "\n" // Next <- Later
			blocks[li].Body = "\n(none yet)\n"
		} else {
			blocks[xi].Body = "\n(none yet)\n"
		}
	} else {
		blocks[xi].Body = "\n(none yet)\n"
	}
	// Archive the completed focus into ## Done (newest first), creating the section if absent.
	entry := "- " + nowStamp() + " — " + oneLine(oldNow)
	if di := idx("Done"); di >= 0 {
		if old := strings.Trim(blocks[di].Body, "\n"); old != "" {
			blocks[di].Body = "\n" + entry + "\n" + old + "\n"
		} else {
			blocks[di].Body = "\n" + entry + "\n"
		}
	} else {
		blocks = append(blocks, mdBlock{Header: "Done", Body: "\n" + entry + "\n"})
	}
	return os.WriteFile(c.roadmapFile(), []byte(renderBlocks(pre, blocks)), 0o644) == nil
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
