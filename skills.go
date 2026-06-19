package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill retrieval: the INDEX (one line per skill) is always shown; full SKILL.md
// bodies are injected only for the skills selected as relevant to the task. This is
// the path that lets memory scale past a handful of skills.
const (
	skillInjectAll = 6 // at/under this many skills, inject them all (no selection)
	skillSelectK   = 4 // otherwise inject the top-K relevant full skills
)

type skillEntry struct{ name, hook string }

// selectSkillsText returns the skills section of the briefing.
func (c *Company) selectSkillsText(a *Agent, t *Task) string {
	entries := c.readIndexEntries()
	if len(entries) == 0 {
		return "(no skills yet)"
	}
	var idx strings.Builder
	idx.WriteString("Skill index (every skill, one line each — open the full skill below if relevant):\n")
	for _, e := range entries {
		idx.WriteString("- " + e.name + " — " + e.hook + "\n")
	}

	var chosen []string
	if len(entries) <= skillInjectAll {
		for _, e := range entries {
			chosen = append(chosen, e.name)
		}
	} else {
		chosen = c.llmSelectSkills(a, t, entries, skillSelectK)
		if len(chosen) == 0 {
			chosen = keywordSelect(t, entries, skillSelectK)
		}
	}
	fmt.Fprintf(os.Stderr, "[skills] %d total; selected for this task: %v\n", len(entries), chosen)

	var b strings.Builder
	b.WriteString(idx.String())
	for _, name := range chosen {
		if body := c.skillBody(name); body != "" {
			b.WriteString("\n--- skill: " + name + " ---\n" + body + "\n")
		}
	}
	return b.String()
}

func (c *Company) readIndexEntries() []skillEntry {
	var out []skillEntry
	for _, ln := range strings.Split(readFileOr(c.skillsIndex(), ""), "\n") {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "- ") {
			continue
		}
		ln = strings.TrimPrefix(ln, "- ")
		name, hook := ln, ""
		if i := strings.Index(ln, " — "); i >= 0 {
			name, hook = strings.TrimSpace(ln[:i]), strings.TrimSpace(ln[i+len(" — "):])
		}
		out = append(out, skillEntry{name: name, hook: hook})
	}
	return out
}

func (c *Company) skillBody(name string) string {
	return readFileOr(filepath.Join(c.skillsDir(), name, "SKILL.md"), "")
}

// llmSelectSkills asks the cheap model which skills are relevant from the index alone.
func (c *Company) llmSelectSkills(a *Agent, t *Task, entries []skillEntry, k int) []string {
	var list strings.Builder
	for _, e := range entries {
		list.WriteString("- " + e.name + ": " + e.hook + "\n")
	}
	prompt := fmt.Sprintf(`A worker is about to do this task:

TITLE: %s
DETAIL: %s

Available skills (lessons / conventions / pitfalls), one per line as "name: hook":
%s
Return ONLY a JSON array (max %d) of the skill NAMES that could affect HOW this task must
be implemented — include any convention or pitfall that applies to code of this kind, even
if the hook is vague. Example: ["a","b"]. If none apply, return [].`,
		t.Title, oneLine(t.Body), list.String(), k)

	out, err := tauComplete(a, prompt)
	if err != nil {
		return nil
	}
	known := map[string]bool{}
	for _, e := range entries {
		known[e.name] = true
	}
	var res []string
	for _, n := range parseStringArray(out) {
		if known[n] && len(res) < k {
			res = append(res, n)
		}
	}
	return res
}

func parseStringArray(s string) []string {
	s = stripFences(s)
	i, j := strings.Index(s, "["), strings.LastIndex(s, "]")
	if i < 0 || j <= i {
		return nil
	}
	var arr []string
	if json.Unmarshal([]byte(s[i:j+1]), &arr) == nil {
		return arr
	}
	return nil
}

// keywordSelect is the fallback when the LLM selector is unavailable.
func keywordSelect(t *Task, entries []skillEntry, k int) []string {
	toks := tokenize(t.Title + " " + t.Body)
	type scored struct {
		name string
		n    int
	}
	var ranked []scored
	for _, e := range entries {
		et := tokenize(e.name + " " + e.hook)
		n := 0
		for tok := range toks {
			if et[tok] {
				n++
			}
		}
		if n > 0 {
			ranked = append(ranked, scored{e.name, n})
		}
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].n > ranked[j].n })
	var res []string
	for _, s := range ranked {
		if len(res) < k {
			res = append(res, s.name)
		}
	}
	return res
}

func tokenize(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if len(w) >= 4 {
			m[w] = true
		}
	}
	return m
}
