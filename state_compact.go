package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	// stateCompactThreshold is the max number of activity-log entries before compaction triggers.
	stateCompactThreshold = 30

	// stateCompactKeepRecent is the number of most-recent entries to retain after compaction,
	// so the log is not empty and the agent can see recent ticks.
	stateCompactKeepRecent = 5
)

// stateSections holds the parsed sections of STATE.md.
type stateSections struct {
	Mission     string
	Shipped     string
	InFlight    string
	Decisions   string
	ActivityLog string
}

// compactState checks whether STATE.md's activity log has grown past the threshold
// and, if so, synthesizes it into the structured sections (Mission, Shipped, In-flight,
// Decisions), keeping only a handful of recent log entries. It is a worker-step
// (no agent involvement) that uses the LLM to do the summarisation.
//
// Compacted STATE.md is written in-place; the previous version is backed up to
// STATE.md.bak. If the LLM synthesis fails the compaction is skipped and the file
// is unchanged (the next tick will retry).
func (c *Company) compactState() error {
	p := c.stateFile()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil // file not yet created — nothing to compact
	}

	content := string(b)
	sections := parseStateSections(content)
	if sections == nil {
		return nil // can't parse sections — skip
	}

	// Count only actual log entries (lines starting with "- ").
	rawLines := strings.Split(strings.TrimSpace(sections.ActivityLog), "\n")
	var entries []string
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			entries = append(entries, line)
		}
	}

	if len(entries) <= stateCompactThreshold {
		return nil // under threshold — nothing to do
	}

	// Ask the LLM to synthesise the log into the structured sections.
	newSections, err := c.synthesizeState(sections, entries)
	if err != nil {
		// Synthesis failed; compaction is best-effort. Log and skip.
		fmt.Fprintf(os.Stderr, "[state] compaction synthesis failed: %v (will retry)\n", err)
		return nil
	}

	// Keep a short tail of recent entries so the log isn't empty.
	keep := entries
	if len(entries) > stateCompactKeepRecent {
		keep = entries[len(entries)-stateCompactKeepRecent:]
	}

	// Rebuild STATE.md.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s — company state\n\n", c.Name))
	sb.WriteString("## Mission\n" + newSections.Mission + "\n\n")
	sb.WriteString("## Shipped\n" + newSections.Shipped + "\n\n")
	sb.WriteString("## In flight\n" + newSections.InFlight + "\n\n")
	sb.WriteString("## Decisions\n" + newSections.Decisions + "\n\n")
	sb.WriteString("## Activity log\n")
	for _, entry := range keep {
		sb.WriteString(entry + "\n")
	}

	// Backup the old state file before overwriting.
	_ = os.WriteFile(p+".bak", b, 0o644)

	if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("writing compacted STATE.md: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[state] compacted %d activity entries → %d, sections updated\n",
		len(entries), len(keep))
	return nil
}

// parseStateSections extracts the five top-level ## sections from STATE.md.
// Returns nil if any required section (Mission, Shipped, In flight, Decisions) is
// missing — compaction will not run on a malformed file.
func parseStateSections(content string) *stateSections {
	sections := &stateSections{}

	// Find sections by ## headings.
	lines := strings.Split(content, "\n")
	var currentSection string
	var sectionLines []string

	sectionMap := map[string]*string{
		"Mission":      &sections.Mission,
		"Shipped":      &sections.Shipped,
		"In flight":    &sections.InFlight,
		"Decisions":    &sections.Decisions,
		"Activity log": &sections.ActivityLog,
	}

	flushSection := func() {
		if currentSection != "" {
			if ptr, ok := sectionMap[currentSection]; ok {
				*ptr = strings.TrimSpace(strings.Join(sectionLines, "\n"))
			}
			sectionLines = nil
		}
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flushSection()
			currentSection = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if currentSection != "" {
			sectionLines = append(sectionLines, line)
		}
	}
	flushSection()

	// Verify all expected sections are present.
	required := []string{"Mission", "Shipped", "In flight", "Decisions"}
	for _, name := range required {
		if *sectionMap[name] == "" {
			return nil
		}
	}
	// Activity log is allowed to be empty (fresh company).

	return sections
}

// synthesizeState calls the LLM to rewrite the structured sections based on the
// accumulated activity log, preserving the Mission (which the CEO owns) and updating
// Shipped / In flight / Decisions from the log.
func (c *Company) synthesizeState(sections *stateSections, entries []string) (*stateSections, error) {
	// Use a lightweight agent — the environment's default provider/model.
	agent := &Agent{
		Name:     "head-of-org-engineering",
		Provider: "deepseek",
		Model:    "deepseek-chat",
	}
	applyModelOverrides(agent)

	logBlock := strings.Join(entries, "\n")
	prompt := fmt.Sprintf(`You are the Head of Org Engineering for a mago company. You maintain the company's STATE.md — the single source of truth about what has shipped, what is in flight, and what decisions have been made.

Current STATE.md sections:

MISSION:
%s

SHIPPED:
%s

IN FLIGHT:
%s

DECISIONS:
%s

Below is the ACTIVITY LOG — one entry per agent tick, oldest first:
%s

Your job: synthesise the activity log into the structured sections above. Follow these rules:

- MISSION: keep verbatim. The CEO owns the mission.
- SHIPPED: list what has been completed (tasks marked done, PRs merged, features shipped). Add any new items visible in the log. Remove items that are no longer relevant.
- IN FLIGHT: list what is actively being worked on (tasks in progress, open PRs, ongoing design). Update based on recent activity in the log.
- DECISIONS: record any notable decisions, trade-offs, or agreements mentioned in the log. Keep past decisions unless superseded.

Return ONLY a JSON object with these exact keys (each value is a string containing 1-4 bullet points or a short paragraph):

{"mission": "value", "shipped": "value", "in_flight": "value", "decisions": "value"}

Be concise. Each field should be a short list or paragraph, not a dump of every log entry.`,
		sections.Mission,
		sections.Shipped,
		sections.InFlight,
		sections.Decisions,
		logBlock,
	)

	out, err := tauComplete(agent, prompt)
	if err != nil {
		return nil, fmt.Errorf("tau call: %w", err)
	}

	// The LLM may wrap its JSON in a ```json fence or return it bare.
	cleaned := stripFences(out)

	var result struct {
		Mission   string `json:"mission"`
		Shipped   string `json:"shipped"`
		InFlight  string `json:"in_flight"`
		Decisions string `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("JSON parse of synthesis: %w\nraw: %s", err, truncate(out, 500))
	}

	updated := &stateSections{
		Mission:   orDefault(result.Mission, sections.Mission),
		Shipped:   orDefault(result.Shipped, sections.Shipped),
		InFlight:  orDefault(result.InFlight, sections.InFlight),
		Decisions: orDefault(result.Decisions, sections.Decisions),
	}
	return updated, nil
}
