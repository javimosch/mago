package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// mode.go is the worker's RUNTIME mode — proactive cadence, non-code comms, and merge policy — read
// live on every decision (not just at startup) and persisted to .mago/mode.json. So a worker can be
// switched between reactive / propose-and-treat / verified-autonomy without a restart: locally with
// `mago mode <…>`, or remotely over the relay (`mago worker mode … --worker <id>`), which writes the
// same file. When the file is absent the mode is derived from the MAGO_* env vars (back-compat).

type workerMode struct {
	Proactive int    `json:"proactive"` // planner propose cadence in secs; 0 = reactive (off)
	Comms     bool   `json:"comms"`     // CMO release note on PR merge (non-code flow)
	Merge     string `json:"merge"`     // review (human merges) | verified (auto-merge on green) | on (auto-merge on approve)
	PRCap     int    `json:"pr_cap"`    // backpressure: stop starting work on a repo at this many open MAGO PRs; 0 = no cap
	IssueCap  int    `json:"issue_cap"` // backpressure: planner stops proposing once the repo has this many open issues; 0 = default (3)
	Update    string `json:"update"`    // self-update: auto (swap binary live on a new release) | manual (default; just nudge)
}

func (c *Company) modeFile() string { return filepath.Join(c.magoDir(), "mode.json") }

// loadMode returns the live mode: the persisted .mago/mode.json if present, else derived from env.
func (c *Company) loadMode() workerMode {
	var m workerMode
	if b, err := os.ReadFile(c.modeFile()); err == nil && json.Unmarshal(b, &m) == nil && m.Merge != "" {
		return m
	}
	m = workerMode{
		Proactive: int(atoiSafe(os.Getenv("MAGO_PROACTIVE"))),
		Comms:     os.Getenv("MAGO_COMMS") == "1",
		PRCap:     int(atoiSafe(os.Getenv("MAGO_PR_CAP"))),
		IssueCap:  int(atoiSafe(os.Getenv("MAGO_ISSUE_CAP"))),
		Update:    os.Getenv("MAGO_UPDATE"),
	}
	switch {
	case os.Getenv("MAGO_NO_MERGE") == "1":
		m.Merge = "review"
	case os.Getenv("MAGO_VERIFY") == "1" || os.Getenv("MAGO_VERIFY_CMD") != "":
		m.Merge = "verified"
	default:
		m.Merge = "on"
	}
	return m
}

func (c *Company) saveMode(m workerMode) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	return os.WriteFile(c.modeFile(), b, 0o644)
}

func (c *Company) modeProactive() int { return c.loadMode().Proactive }
func (c *Company) modeComms() bool    { return c.loadMode().Comms }
func (c *Company) modeMerge() string  { return c.loadMode().Merge }
func (c *Company) modePRCap() int     { return c.loadMode().PRCap }
func (c *Company) modeIssueCap() int  { return c.loadMode().IssueCap }

// modeUpdate is the self-update policy, normalized: "auto" or "manual" (the default).
func (c *Company) modeUpdate() string {
	if c.loadMode().Update == "auto" {
		return "auto"
	}
	return "manual"
}

func describeMode(m workerMode) string {
	p := "off (reactive)"
	if m.Proactive > 0 {
		p = fmt.Sprintf("every %ds", m.Proactive)
	}
	s := fmt.Sprintf("proactive %s · comms %s · merge %s", p, onOff(m.Comms), m.Merge)
	if m.PRCap > 0 {
		s += fmt.Sprintf(" · pr-cap %d", m.PRCap)
	}
	if m.IssueCap > 0 {
		s += fmt.Sprintf(" · issue-cap %d", m.IssueCap)
	}
	if m.Update == "auto" {
		s += " · update auto"
	}
	return s
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// parseMode applies preset/key=value tokens onto a base mode. Presets: reactive, proactive, review,
// verified|auto. Pairs: proactive=<secs>, comms=on|off, merge=review|verified|on, pr-cap=<n>,
// issue-cap=<n> (0 disables a cap), update=auto|manual.
func parseMode(base workerMode, tokens []string) (workerMode, error) {
	m := base
	for _, tok := range tokens {
		t := strings.TrimSpace(tok)
		switch t {
		case "":
		case "reactive":
			m.Proactive = 0
		case "proactive":
			if m.Proactive <= 0 {
				m.Proactive = 3600
			}
		case "review", "review-only":
			m.Merge = "review"
		case "verified", "auto":
			m.Merge = "verified"
		case "comms-on":
			m.Comms = true
		case "comms-off":
			m.Comms = false
		default:
			k, v, ok := strings.Cut(t, "=")
			if !ok {
				return m, fmt.Errorf("unknown mode token %q (try: reactive | proactive[=secs] | review | verified | comms=on|off | merge=review|verified|on | pr-cap=N | issue-cap=N | update=auto|manual)", t)
			}
			switch k {
			case "proactive":
				m.Proactive = int(atoiSafe(v))
			case "comms":
				m.Comms = v == "on" || v == "true" || v == "1"
			case "merge":
				if v != "review" && v != "verified" && v != "on" {
					return m, fmt.Errorf("merge must be review|verified|on, got %q", v)
				}
				m.Merge = v
			case "pr-cap", "prs":
				m.PRCap = int(atoiSafe(v))
			case "issue-cap", "issues":
				m.IssueCap = int(atoiSafe(v))
			case "update":
				if v != "auto" && v != "manual" {
					return m, fmt.Errorf("update must be auto|manual, got %q", v)
				}
				m.Update = v
			default:
				return m, fmt.Errorf("unknown mode key %q", k)
			}
		}
	}
	if m.Merge == "" {
		m.Merge = "review"
	}
	return m, nil
}

// applyControl handles a relay "control" frame: {"tokens":[...]} from `mago worker mode`. It applies
// the tokens onto the live mode and persists them — the running worker picks it up with no restart.
func (c *Company) applyControl(body []byte) {
	var ctl struct {
		Tokens []string `json:"tokens"`
	}
	if json.Unmarshal(body, &ctl) != nil || len(ctl.Tokens) == 0 {
		return
	}
	m, err := parseMode(c.loadMode(), ctl.Tokens)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[mode] control rejected: %v\n", err)
		return
	}
	if err := c.saveMode(m); err != nil {
		fmt.Fprintf(os.Stderr, "[mode] control save failed: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "[mode] updated via relay -> %s\n", describeMode(m))
}

// cmdMode is the LOCAL switch: `mago mode [show | <tokens...>] [-C dir]`. A running worker picks the
// change up live (no restart). For a remote worker use `mago worker mode … --worker <id>`.
func cmdMode(args []string) error {
	dir, rest, err := parseCompanyDir(args)
	if err != nil {
		return err
	}
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	if len(rest) == 0 || rest[0] == "show" {
		fmt.Printf("mode: %s\n", describeMode(comp.loadMode()))
		return nil
	}
	m, err := parseMode(comp.loadMode(), rest)
	if err != nil {
		return err
	}
	if err := comp.saveMode(m); err != nil {
		return err
	}
	fmt.Printf("mode set: %s\n(a running worker applies it live within ~30s; no restart needed)\n", describeMode(m))
	return nil
}
