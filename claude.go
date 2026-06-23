package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// claude.go drives Claude Code (the `claude` CLI) as an alternative agent harness to tau. Selected
// per-agent with `provider: claude` (model e.g. `sonnet`) or globally via MAGO_PROVIDER=claude
// MAGO_MODEL=sonnet. Auth is the local Claude Code subscription — no API key. Print mode
// (`-p --output-format json`) returns a single result object; for ticks, tools run under
// bypassPermissions so the agent can use bash/git/gh and edit files non-interactively.

func claudeModel(a *Agent) string {
	if strings.TrimSpace(a.Model) != "" {
		return a.Model
	}
	return "sonnet"
}

// claudeResult extracts the final assistant text from `claude --output-format json` output. claude
// prints this object even on a non-zero exit, so callers feed it the raw stdout regardless of exit
// code. Not-logged-in gets an actionable hint (the #1 dogfood gotcha: a custom HOME hides the auth).
func claudeResult(out []byte) (string, error) {
	var r struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
		Subtype string `json:"subtype"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &r) != nil {
		return "", fmt.Errorf("claude: no/garbled output — is `claude` installed and logged in? " +
			"(if mago runs under a custom HOME, set CLAUDE_CONFIG_DIR=~/.claude)")
	}
	if r.IsError || strings.TrimSpace(r.Result) == "" {
		if strings.Contains(r.Result, "Not logged in") || strings.Contains(r.Result, "/login") {
			return "", fmt.Errorf("claude not authenticated — run `claude /login`, or set " +
				"CLAUDE_CONFIG_DIR to your real ~/.claude if mago runs under a custom HOME")
		}
		return "", fmt.Errorf("claude error (%s): %s", r.Subtype, oneLine(truncate(r.Result, 200)))
	}
	return r.Result, nil
}

// runClaude drives one tick via Claude Code: the agent persona is appended to Claude Code's system
// prompt, the briefing is the prompt, tools run under bypassPermissions, and the final message
// (expected to be the reflection JSON, per the briefing) is returned for the caller to parse.
func runClaude(workspace string, a *Agent, systemPrompt, userPrompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "-p", userPrompt,
		"--model", claudeModel(a),
		"--output-format", "json",
		"--permission-mode", "bypassPermissions",
		"--append-system-prompt", systemPrompt)
	cmd.Dir = workspace
	cmd.Env = os.Environ()
	// Claude Code refuses bypassPermissions when running as root ("...cannot be used with root/sudo
	// privileges"). A dedicated worker box often runs as root and has explicitly opted into autonomous
	// tool use, so signal a sandboxed context to let the agent use its tools. (Honors an explicit IS_SANDBOX.)
	if os.Geteuid() == 0 && os.Getenv("IS_SANDBOX") == "" {
		cmd.Env = append(cmd.Env, "IS_SANDBOX=1")
	}
	out, _ := cmd.Output() // claude prints the result JSON even on non-zero exit; claudeResult judges it
	return claudeResult(out)
}

// claudeComplete is the lightweight, no-tools completion (routing, planning, review verdicts,
// release notes). Retries on transient failures, mirroring tauComplete.
func claudeComplete(a *Agent, prompt string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		cmd := exec.CommandContext(ctx, "claude", "-p", prompt,
			"--model", claudeModel(a), "--output-format", "json")
		cmd.Env = os.Environ()
		out, _ := cmd.Output() // result JSON is printed even on non-zero exit
		cancel()
		r, perr := claudeResult(out)
		if perr == nil {
			return r, nil
		}
		lastErr = perr
	}
	return "", lastErr
}
