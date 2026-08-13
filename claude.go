package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// errClaudeTransient marks a retryable failure: claude emitted no/garbled output, an empty result, or
// an overload/rate-limit/timeout — process-level hiccups (common under load) that a retry usually
// clears. Auth and genuine model errors are NOT wrapped with this, so callers don't burn retries on them.
var errClaudeTransient = errors.New("claude: transient failure (no/garbled output or overload)")

func transientClaude(err error) bool { return errors.Is(err, errClaudeTransient) }

func overloadish(s string) bool {
	l := strings.ToLower(s)
	for _, m := range []string{"overloaded", "rate limit", "rate_limit", "try again", "timeout", "timed out", "503", "529", "502", "connection reset"} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

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
		// No parseable JSON at all — claude crashed / produced nothing (often overload). Transient,
		// but keep the actionable hint for the case it's actually a missing/unauthenticated CLI.
		return "", fmt.Errorf("%w — if persistent, check `claude` is installed and logged in "+
			"(custom HOME? set CLAUDE_CONFIG_DIR=~/.claude)", errClaudeTransient)
	}
	if r.IsError || strings.TrimSpace(r.Result) == "" {
		if strings.Contains(r.Result, "Not logged in") || strings.Contains(r.Result, "/login") {
			return "", fmt.Errorf("claude not authenticated — run `claude /login`, or set " +
				"CLAUDE_CONFIG_DIR to your real ~/.claude if mago runs under a custom HOME")
		}
		if strings.TrimSpace(r.Result) == "" || overloadish(r.Result) {
			return "", fmt.Errorf("%w (subtype %q)", errClaudeTransient, r.Subtype)
		}
		return "", fmt.Errorf("claude error (%s): %s", r.Subtype, oneLine(truncate(r.Result, 200)))
	}
	return r.Result, nil
}

// withClaudeRetry runs do() up to maxAttempts times, retrying ONLY transient failures with
// exponential backoff (base, 2·base, 4·base…). Auth / genuine errors return immediately.
func withClaudeRetry(maxAttempts int, base time.Duration, do func() (string, error)) (string, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(base << (attempt - 1)) // base, 2·base, 4·base…
		}
		r, err := do()
		if err == nil {
			return r, nil
		}
		lastErr = err
		if !transientClaude(err) {
			return "", err // auth / real model error — don't waste retries
		}
		fmt.Fprintf(os.Stderr, "[claude] transient failure (attempt %d/%d): %v\n", attempt+1, maxAttempts, err)
	}
	return "", lastErr
}

// runClaude drives one tick via Claude Code: the agent persona is appended to Claude Code's system
// prompt, the briefing is the prompt (piped via stdin to avoid CLI length limits), tools run under
// bypassPermissions, and the final message (expected to be the reflection JSON, per the briefing) is
// returned for the caller to parse.
func runClaude(workspace string, a *Agent, systemPrompt, userPrompt string) (string, error) {
	// Retry transient hiccups: a garbled/empty/overload failure means claude crashed before doing
	// work (it prints result JSON even on a normal error exit), so re-running is safe and doesn't
	// repeat side effects. Longer backoff than completions since a tick is heavier.
	return withClaudeRetry(3, 3*time.Second, func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "claude",
			"--model", claudeModel(a),
			"--output-format", "json",
			"--permission-mode", "bypassPermissions",
			"--append-system-prompt", systemPrompt)
		cmd.Dir = workspace
		cmd.Env = os.Environ()
		// Claude Code refuses bypassPermissions when running as root ("...cannot be used with root/sudo
		// privileges"). A dedicated worker box often runs as root and has explicitly opted into autonomous
		// tool use, so signal a sandboxed context to let the agent use its tools. (Honors explicit IS_SANDBOX.)
		if os.Geteuid() == 0 && os.Getenv("IS_SANDBOX") == "" {
			cmd.Env = append(cmd.Env, "IS_SANDBOX=1")
		}
		// Feed the prompt through stdin to avoid shell arg length limits. Using a
		// Reader avoids the pipe-buffer deadlock that can occur when a large prompt
		// is written before the child process has started.
		cmd.Stdin = strings.NewReader(userPrompt)
		out, runErr := cmd.Output() // claude prints the result JSON even on non-zero exit; claudeResult judges it
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "[claude] command error: %v\n", runErr)
		}
		return claudeResult(out)
	})
}

// claudeComplete is the lightweight, no-tools completion (routing, planning, review verdicts,
// release notes). Retries on transient failures, mirroring tauComplete.
func claudeComplete(a *Agent, prompt string) (string, error) {
	// 4 attempts, exponential backoff (2s,4s,8s), retrying only transient failures.
	return withClaudeRetry(4, 2*time.Second, func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "claude",
			"--model", claudeModel(a), "--output-format", "json")
		cmd.Env = os.Environ()
		// Use a Reader for stdin so large prompts are streamed as the child
		// reads, rather than blocking the parent before it has started.
		cmd.Stdin = strings.NewReader(prompt)
		out, runErr := cmd.Output() // result JSON is printed even on non-zero exit
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "[claude] command error: %v\n", runErr)
		}
		return claudeResult(out)
	})
}
