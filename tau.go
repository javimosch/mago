package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// runTau drives tau for one tick: a stateless (no --session) single-shot call so
// the agent's only knowledge of the past is the injected briefing. Returns the
// final content (which, under --schema, is the reflection JSON).
func runTau(workspace string, a *Agent, systemPrompt, userPrompt string) (string, error) {
	args := []string{
		"-p",
		"--provider", a.Provider,
		"--model", a.Model,
		"--mode", "json",
		"--tools", "bash,read,write,edit",
		"--max-iterations", "30",
		"--system-prompt", systemPrompt,
		userPrompt,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tau", args...)
	cmd.Dir = workspace
	cmd.Env = os.Environ()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting tau (is it on PATH?): %w", err)
	}
	var lines []string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1024*1024), 32*1024*1024)
	for sc.Scan() {
		ln := sc.Text()
		lines = append(lines, ln)
		emitProgress(ln)
	}
	waitErr := cmd.Wait()
	fmt.Fprintln(os.Stderr)
	content, perr := extractFinalContent(lines)
	if content == "" {
		if waitErr != nil {
			return "", fmt.Errorf("tau failed: %w", waitErr)
		}
		return "", perr
	}
	return content, nil
}

// tauComplete is a lightweight one-shot call (no tools, no stream) used for
// auxiliary reasoning like skill selection. Returns the model's content.
func tauComplete(a *Agent, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tau", "-p",
		"--provider", a.Provider, "--model", a.Model,
		"--no-tools", "--no-stream", "--mode", "json", prompt)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var m map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(lines[i])), &m) == nil {
			if c, ok := m["content"].(string); ok && c != "" {
				return c, nil
			}
		}
	}
	return "", fmt.Errorf("no content from tau")
}

// emitProgress streams the model's text chunks to stderr so the human can watch.
func emitProgress(line string) {
	var m map[string]any
	if json.Unmarshal([]byte(line), &m) != nil {
		return
	}
	if c, ok := m["chunk"].(string); ok {
		fmt.Fprint(os.Stderr, c)
	}
}

// extractFinalContent reads tau's NDJSON: prefer the done:true line's content,
// else fall back to concatenated chunks.
func extractFinalContent(lines []string) (string, error) {
	var final string
	var sb strings.Builder
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(t), &m) != nil {
			continue
		}
		if c, ok := m["chunk"].(string); ok {
			sb.WriteString(c)
		}
		if d, ok := m["done"].(bool); ok && d {
			if c, ok := m["content"].(string); ok && c != "" {
				final = c
			}
		}
	}
	if final != "" {
		return final, nil
	}
	if sb.Len() > 0 {
		return sb.String(), nil
	}
	return "", fmt.Errorf("no output from tau")
}

// parseReflection extracts the reflection from tau's output, tolerating prose
// around it. Tries the last fenced ```json block first, then fallbacks.
func parseReflection(content string) (*Reflection, error) {
	for _, cand := range jsonCandidates(content) {
		var r Reflection
		if json.Unmarshal([]byte(cand), &r) == nil && r.Summary != "" {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("could not parse reflection JSON: %s", truncate(content, 300))
}

// jsonCandidates yields likely JSON-object substrings: fenced blocks (last first),
// then the whole stripped string, then the outermost { ... } span.
func jsonCandidates(s string) []string {
	var fenced []string
	parts := strings.Split(s, "```")
	for i := 1; i < len(parts); i += 2 {
		seg := strings.TrimSpace(parts[i])
		seg = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(seg, "json"), "JSON"))
		fenced = append(fenced, seg)
	}
	out := make([]string, 0, len(fenced)+2)
	for i := len(fenced) - 1; i >= 0; i-- { // last fenced block is the reflection
		out = append(out, fenced[i])
	}
	st := stripFences(s)
	out = append(out, st)
	if i := strings.Index(st, "{"); i >= 0 {
		if j := strings.LastIndex(st, "}"); j > i {
			out = append(out, st[i:j+1])
		}
	}
	return out
}
