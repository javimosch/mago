package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// workerActions is the canonical list of `mago worker` sub-actions, used for the
// unknown-action "did you mean" suggestion. Keep in sync with the switch below
// (TestWorkerActionsSuggestThemselves guards against drift).
var workerActions = []string{"doctor", "mode"}

func cmdWorker(args []string) error {
	if len(args) < 1 {
		return &cliErr{80, "usage: mago worker <subcommand> (doctor, mode)\nrun `mago worker --help` for usage"}
	}
	switch args[0] {
	case "doctor":
		workerDoctor()
	case "mode":
		return cmdWorkerMode(args[1:])
	default:
		if s := nearestAction(args[0], workerActions); s != "" {
			return &cliErr{80, fmt.Sprintf("unknown worker subcommand %q — did you mean %q?\nrun `mago worker --help` for usage", args[0], s)}
		}
		return &cliErr{80, fmt.Sprintf("unknown worker subcommand %q (valid: doctor, mode)\nrun `mago worker --help` for usage", args[0])}
	}
	return nil
}

// cmdWorkerMode switches a REMOTE worker's mode live over the relay (no ssh, no restart):
//
//	mago worker mode <reactive|proactive[=secs]|review|verified|comms=on|off …> (--worker <id> | --all)
func cmdWorkerMode(args []string) error {
	worker, all := "", false
	var tokens []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--worker", "-w":
			if i+1 < len(args) {
				worker = args[i+1]
				i++
			}
		case "--all":
			all = true
		default:
			tokens = append(tokens, args[i])
		}
	}
	if len(tokens) == 0 {
		return fmt.Errorf("usage: mago worker mode <reactive|proactive[=secs]|review|verified|comms=on|off> (--worker <id> | --all)")
	}
	if worker == "" && !all {
		return fmt.Errorf("specify --worker <id> (its MAGO_WORKER_ID / hostname) or --all")
	}
	if _, err := parseMode(workerMode{Merge: "review"}, tokens); err != nil { // validate before sending
		return err
	}
	cfg := loadConfig()
	var out struct {
		Updated int      `json:"updated"`
		Workers []string `json:"workers"`
	}
	body := map[string]any{"worker": worker, "all": all, "tokens": tokens}
	if err := cfg.platformDo("POST", "/api/worker/control", body, true, &out); err != nil {
		return err
	}
	if out.Updated == 0 {
		fmt.Printf("no connected worker matched (%s) — is it running with --relay?\n", ifStr(all, "--all", worker))
		return nil
	}
	fmt.Printf("mode pushed live to %d worker(s): %v\n", out.Updated, out.Workers)
	return nil
}

type diagCheck struct {
	label string
	ok    bool
	hint  string
}

// providerCheckNames returns the logical names of the checks that will be run for the
// given provider value (MAGO_PROVIDER). "claude" → claude-specific checks; everything
// else (including "") → tau/opencode checks. Used by tests to verify selection logic
// without executing the checks.
func providerCheckNames(provider string) []string {
	if provider == "claude" {
		return []string{"claude-on-path", "claude-auth"}
	}
	return []string{"tau-on-path", "opencode-api-key"}
}

// workerDoctor validates that the configured LLM harness, gh, and any required
// API keys are present. Provider-aware: reads MAGO_PROVIDER and runs the appropriate
// checks (tau+OPENCODE_API_KEY for opencode/tau workers; claude+auth for claude workers).
// When MAGO_GH_REPO is set (GitHub-backed mode), also checks MAGO_GH_TOKEN.
// Prints a pass/fail line per check with a fix hint on failure.
// Exits 101 if any check fails (integration error per AGENTS.md exit code map).
func workerDoctor() {
	provider := os.Getenv("MAGO_PROVIDER")

	var checks []diagCheck
	if provider == "claude" {
		checks = append(checks, checkClaudeOnPath(), checkClaudeAuth())
	} else {
		checks = append(checks, checkTau(), checkOpenCodeAPIKey())
	}
	checks = append(checks, checkGhOnPath(), checkGhAuth())
	if ghRepo := strings.TrimSpace(os.Getenv("MAGO_GH_REPO")); ghRepo != "" {
		checks = append(checks, checkGHToken())
	}

	failed := 0
	for _, c := range checks {
		if c.ok {
			fmt.Printf("  [ok]   %s\n", c.label)
		} else {
			fmt.Printf("  [fail] %s\n         fix: %s\n", c.label, c.hint)
			failed++
		}
	}

	fmt.Println()
	if failed > 0 {
		fmt.Printf("%d check(s) failed — fix the above and re-run `mago worker doctor`\n", failed)
		os.Exit(101)
	}
	fmt.Println("All checks passed. Worker is ready.")
}

func checkTau() diagCheck {
	if _, err := exec.LookPath("tau"); err != nil {
		return diagCheck{
			label: "tau (LLM driver) on PATH",
			ok:    false,
			hint:  "install tau from https://opencode.ai then ensure it is on your PATH",
		}
	}
	return diagCheck{label: "tau (LLM driver) on PATH", ok: true}
}

func checkGhOnPath() diagCheck {
	if _, err := exec.LookPath("gh"); err != nil {
		return diagCheck{
			label: "gh (GitHub CLI) on PATH",
			ok:    false,
			hint:  "install gh from https://cli.github.com then re-run",
		}
	}
	return diagCheck{label: "gh (GitHub CLI) on PATH", ok: true}
}

func checkGhAuth() diagCheck {
	if _, err := exec.LookPath("gh"); err != nil {
		// gh not installed — auth check is moot; already reported above
		return diagCheck{
			label: "gh authenticated",
			ok:    false,
			hint:  "install gh first, then run: gh auth login",
		}
	}
	cmd := exec.Command("gh", "auth", "status")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return diagCheck{
			label: "gh authenticated",
			ok:    false,
			hint:  "run: gh auth login",
		}
	}
	return diagCheck{label: "gh authenticated", ok: true}
}

func checkOpenCodeAPIKey() diagCheck {
	if os.Getenv("OPENCODE_API_KEY") == "" {
		return diagCheck{
			label: "OPENCODE_API_KEY set",
			ok:    false,
			hint:  "export OPENCODE_API_KEY=<your-key>  (get one at opencode.ai)",
		}
	}
	return diagCheck{label: "OPENCODE_API_KEY set", ok: true}
}

func checkClaudeOnPath() diagCheck {
	if _, err := exec.LookPath("claude"); err != nil {
		return diagCheck{
			label: "claude (Claude Code CLI) on PATH",
			ok:    false,
			hint:  "install Claude Code from https://claude.ai/code then ensure it is on your PATH",
		}
	}
	return diagCheck{label: "claude (Claude Code CLI) on PATH", ok: true}
}

// checkClaudeAuth runs a print-mode probe to confirm the local Claude Code subscription
// is authenticated. Uses claudeResult's "Not logged in" detection (the #1 setup issue
// on a custom HOME where CLAUDE_CONFIG_DIR is not set).
func checkClaudeAuth() diagCheck {
	if _, err := exec.LookPath("claude"); err != nil {
		// claude not on PATH — path check already reported this
		return diagCheck{
			label: "claude authenticated",
			ok:    false,
			hint:  "install claude first, then run: claude /login",
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "-p", "ping", "--output-format", "json")
	cmd.Env = os.Environ()
	out, runErr := cmd.Output()
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "[claude] auth probe error: %v\n", runErr)
	}
	if _, err := claudeResult(out); err != nil {
		if transientClaude(err) {
			return diagCheck{
				label: "claude authenticated",
				ok:    false,
				hint:  "probe returned a transient error — re-run to confirm, or check: claude /login",
			}
		}
		return diagCheck{
			label: "claude authenticated",
			ok:    false,
			hint:  "run: claude /login  (custom HOME? set CLAUDE_CONFIG_DIR=~/.claude)",
		}
	}
	return diagCheck{label: "claude authenticated", ok: true}
}

// checkGHToken verifies that MAGO_GH_TOKEN is set when in GitHub-backed mode.
// An absent token means mago falls back to gh's own auth, which may silently
// fail in CI or on a fresh machine. This check makes the missing token explicit.
func checkGHToken() diagCheck {
	if os.Getenv("MAGO_GH_TOKEN") == "" {
		return diagCheck{
			label: "MAGO_GH_TOKEN set",
			ok:    false,
			hint: "export MAGO_GH_TOKEN=<your-pat>  " +
				"(create a Personal Access Token with repo scope at https://github.com/settings/tokens?type=legacy)",
		}
	}
	return diagCheck{label: "MAGO_GH_TOKEN set", ok: true}
}
