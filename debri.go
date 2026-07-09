package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// debri.go drives debri (https://github.com/javimosch/debri) — a Go CLI that wraps devin, a
// separate agentic coding CLI — as an alternative agent harness alongside tau, pi, and Claude
// Code. Select it per-agent with `provider: debri` (model e.g. "SWE-1.6"), or globally via
// MAGO_PROVIDER=debri MAGO_MODEL=SWE-1.6. Auth is devin's own login (`devin auth login`) — no
// API key of mago's or debri's own.
//
// Requires debri v1.1.0+ (https://github.com/javimosch/debri/releases): earlier builds could
// collide tmux session names under concurrent spawns, report a crashed/killed session as a false
// success, and lacked process-exit completion detection (relying only on a silence timer, which
// can't tell "the agent is still working" from "it's done").
//
// Unlike tau/pi/claude, devin has no separate system-prompt flag — debri takes exactly one
// prompt. The persona/contract (systemPrompt) and the tick briefing (userPrompt) are combined
// into a single prompt, written to a temp file and passed via debri's --file (avoids both shell
// arg-length limits and escaping bugs for a multi-KB briefing).
//
// debri spins up a fresh tmux+devin session per call — heavier per-invocation than tau/pi/claude.
// Best suited to implementer-style ticks where that overhead is amortized over real coding work;
// avoid it for routing/planning agents that make many small completion calls.

const debriStableTimeoutTick = "600000" // 10min safety cap; process-exit detection is the real signal

// debriStableTimeoutComplete: 150s, not the more obvious 60-90s. Caught live: under
// --permission-mode auto, devin can take noticeably longer to actually exit its process after
// printing its final reply than it does under --permission-mode dangerous, so a short cap makes
// the SLOWER stable-timeout fallback win the race against process-exit detection — and that
// fallback path captures the pane on a fixed poll cadence, which can pick up trailing terminal
// noise (observed: a shell prompt line) appended after devin's real response. Give process-exit
// enough headroom to win first in the normal case; debriCompleteContextTimeout below is the hard
// backstop if it still doesn't.
const debriStableTimeoutComplete = "150000"

// debriCompleteContextTimeout bounds debriComplete's whole process — was previously unbounded
// (unlike tauComplete/claudeComplete, which both use a 3-minute context), so a wedged devin call
// could hang mago forever. Set above debriStableTimeoutComplete so the safety cap inside debri
// itself is what normally fires, not this outer kill.
const debriCompleteContextTimeout = 3 * time.Minute

// debriModel returns --model, or "" to let devin use its own default/adaptive model when unset.
func debriModel(a *Agent) string {
	return strings.TrimSpace(a.Model)
}

// combineDebriPrompt merges the persona/contract and the tick briefing into the single prompt
// devin receives — devin has no separate system-prompt concept.
// debriReflectionReminder reinforces the fenced-json reflection requirement at the very end of
// the combined prompt. tau/pi/claude keep it as a genuinely separate system-role message the
// model attends to independently of the user turn; devin gets one flat prompt, and the
// instruction otherwise sits only in the systemPrompt half (persona + operating contract), well
// before the actual task briefing. Caught live: across three fresh tasks spanning trivial (write
// one file) to real (implement + verify a small script, correctly), devin/SWE-1.6 consistently
// did the actual work right but never closed with a parseable reflection on the first pass — a
// prompt-recency issue, not a devin/debri capability gap. A short, direct reminder positioned
// last (where a model's attention is freshest right before it starts generating) is the standard
// mitigation.
const debriReflectionReminder = "\n\n---\n\nBefore you finish: end your reply with your " +
	"reflection as ONE fenced ```json code block (the exact schema is above) and nothing after it. " +
	"This is required even for a small or already-complete task."

func combineDebriPrompt(systemPrompt, userPrompt string) string {
	return systemPrompt + "\n\n---\n\n" + userPrompt + debriReflectionReminder
}

// writeDebriPromptFile writes prompt to a temp file for debri's --file flag, returning the path
// and a cleanup func. A file (not a positional arg) avoids shell arg-length limits and quoting
// bugs for a multi-KB briefing.
func writeDebriPromptFile(prompt string) (string, func(), error) {
	f, err := os.CreateTemp("", "mago-debri-prompt-*.txt")
	if err != nil {
		return "", nil, err
	}
	if _, err := f.WriteString(prompt); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", nil, err
	}
	f.Close()
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

// runDebri drives one tick via debri/devin: the persona+briefing is combined into one prompt,
// tools run under --permission-mode dangerous (mago's worker is already an autonomous, non-
// interactive context — same posture as runClaude's bypassPermissions), and the final content
// (expected to be the reflection JSON per the briefing) is returned for the caller to parse.
func runDebri(workspace string, a *Agent, systemPrompt, userPrompt string) (string, error) {
	promptFile, cleanup, err := writeDebriPromptFile(combineDebriPrompt(systemPrompt, userPrompt))
	if err != nil {
		return "", fmt.Errorf("debri: writing prompt file: %w", err)
	}
	defer cleanup()

	args := []string{
		"--working-dir", workspace,
		"--permission-mode", "dangerous",
		"--stable-timeout", debriStableTimeoutTick,
		"--stream",
		"--file", promptFile,
	}
	if m := debriModel(a); m != "" {
		args = append(args, "--model", m)
	}

	cmd := exec.Command("debri", args...)
	cmd.Env = os.Environ()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting debri (is it on PATH? see .agents/skills/agent-runtime.md): %w", err)
	}
	var lines []string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1024*1024), 32*1024*1024)
	for sc.Scan() {
		ln := sc.Text()
		lines = append(lines, ln)
		emitProgressDebri(ln)
	}
	waitErr := cmd.Wait()
	fmt.Fprintln(os.Stderr)
	content, derr := extractDebriFinalContent(lines)
	if derr != nil {
		// A v1.1.0+ debri reports a crashed/killed devin session as a real error event rather
		// than a false {"event":"done"} — surface it as-is rather than the generic wait error.
		return "", fmt.Errorf("debri failed: %w", derr)
	}
	if content == "" && waitErr != nil {
		return "", fmt.Errorf("debri failed: %w", waitErr)
	}
	return content, nil
}

// debriComplete is a lightweight one-shot call (routing, planning, review verdicts, release
// notes, tick.go's reflection recovery), mirroring tauComplete/claudeComplete. Two safety layers,
// because devin has no true no-tools mode (unlike tau/pi's --no-tools): --permission-mode auto
// (auto-approves read-only tools only, never edits) blocks writes outright, AND the call runs in
// a dedicated, ephemeral scratch directory rather than mago's own ambient cwd. Without the scratch
// dir, devin still has live tool access and WILL explore/verify the filesystem it's sitting in —
// caught live: a recovery-call prompt referencing a task's workspace (as plain text, since these
// prompts were designed for tau/claude's true no-tools reasoning) led devin to `ls` mago's own
// source directory (the process cwd), not find the file being asked about, and try to write it
// there — rejected by --permission-mode auto, so no damage, but real wasted tool-call turns and a
// gap this scratch dir closes for good (nothing meaningful there for it to find or touch either
// way). A shorter stable-timeout keeps it from ballooning into a full tick. Simple retry on any
// failure (debri has no rich transient/auth classification like claude.go's, so — mirroring
// tauComplete — every failure gets the same bounded retry).
func debriComplete(a *Agent, prompt string) (string, error) {
	promptFile, cleanup, err := writeDebriPromptFile(prompt)
	if err != nil {
		return "", fmt.Errorf("debri: writing prompt file: %w", err)
	}
	defer cleanup()

	scratchDir, err := os.MkdirTemp("", "mago-debri-complete-*")
	if err != nil {
		return "", fmt.Errorf("debri: creating scratch dir: %w", err)
	}
	defer os.RemoveAll(scratchDir)

	args := []string{
		"--working-dir", scratchDir,
		"--permission-mode", "auto",
		"--stable-timeout", debriStableTimeoutComplete,
		"--json",
		"--file", promptFile,
	}
	if m := debriModel(a); m != "" {
		args = append(args, "--model", m)
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second) // 2s, 4s backoff
		}
		ctx, cancel := context.WithTimeout(context.Background(), debriCompleteContextTimeout)
		cmd := exec.CommandContext(ctx, "debri", args...)
		cmd.Env = os.Environ()
		out, err := cmd.Output()
		cancel()
		content, cerr := debriJSONResult(out)
		if cerr == nil && content != "" {
			return content, nil
		}
		if cerr != nil {
			lastErr = cerr
		} else if err != nil {
			lastErr = fmt.Errorf("debri failed: %w", err)
		} else {
			lastErr = fmt.Errorf("debri: empty content")
		}
	}
	return "", lastErr
}

// emitProgressDebri streams debri's chunk events to stderr so the human can watch in real time.
func emitProgressDebri(line string) {
	var m struct {
		Event   string `json:"event"`
		Content string `json:"content"`
	}
	if json.Unmarshal([]byte(line), &m) != nil {
		return
	}
	if m.Event == "chunk" {
		fmt.Fprint(os.Stderr, m.Content)
	}
}

// extractDebriFinalContent reads debri's --stream NDJSON. A non-nil error means debri itself
// reported {"event":"error"} — a real failure (crashed/killed session, devin never started), not
// merely "no output found" — so the caller should NOT fall back to treating it as empty success.
func extractDebriFinalContent(lines []string) (string, error) {
	var sb strings.Builder
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		var m struct {
			Event   string `json:"event"`
			Content string `json:"content"`
			Error   string `json:"error"`
		}
		if json.Unmarshal([]byte(t), &m) != nil {
			continue
		}
		switch m.Event {
		case "chunk":
			sb.WriteString(m.Content)
		case "done":
			if m.Content != "" {
				return m.Content, nil
			}
			if sb.Len() > 0 {
				return sb.String(), nil
			}
			return "", nil // legitimately empty output — not an error
		case "error":
			return "", fmt.Errorf("%s", m.Error)
		}
	}
	if sb.Len() > 0 {
		return sb.String(), nil
	}
	return "", fmt.Errorf("no output from debri")
}

// debriJSONResult parses debri's --json single-object output (used by debriComplete):
// {"content":"...","elapsed_ms":N} on success, {"error":"...","elapsed_ms":N} on failure.
func debriJSONResult(out []byte) (string, error) {
	var m struct {
		Content string `json:"content"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(out), &m) != nil {
		return "", fmt.Errorf("debri: no parseable JSON output")
	}
	if m.Error != "" {
		return "", fmt.Errorf("%s", m.Error)
	}
	return m.Content, nil
}
