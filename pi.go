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

// pi.go drives pi (https://pi.dev) as a third agent harness alongside tau and the Claude Code CLI.
// Select it per-agent with `provider: pi` in agent frontmatter (or MAGO_PROVIDER=pi globally).
// The model field uses pi's "provider/model" shorthand, e.g. "openrouter/deepseek/deepseek-chat-v3-0324",
// or a short name pi can resolve like "deepseek-v4-flash".
//
// Pi's CLI interface is nearly identical to tau (-p, --mode json, --tools, --system-prompt) but its
// NDJSON output schema is richer: it uses type:agent_end / type:turn_end events rather than done:true.

func runPi(workspace string, a *Agent, systemPrompt, userPrompt string) (string, error) {
	args := []string{
		"-p",
		"--model", a.Model,
		"--mode", "json",
		"--tools", "bash,read,write,edit",
		"--system-prompt", systemPrompt,
		userPrompt,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pi", args...)
	cmd.Dir = workspace
	cmd.Env = os.Environ()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting pi (is it on PATH?): %w", err)
	}
	var lines []string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1024*1024), 32*1024*1024)
	for sc.Scan() {
		ln := sc.Text()
		lines = append(lines, ln)
		emitProgressPi(ln)
	}
	waitErr := cmd.Wait()
	fmt.Fprintln(os.Stderr)
	content, perr := extractPiFinalContent(lines)
	if content == "" {
		if waitErr != nil {
			return "", fmt.Errorf("pi failed: %w", waitErr)
		}
		return "", perr
	}
	return content, nil
}

// piComplete is the lightweight, no-tools completion used for routing, planning, and review
// verdicts. Retries transient failures with backoff, mirroring tauComplete.
func piComplete(a *Agent, prompt string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		cmd := exec.CommandContext(ctx, "pi", "-p",
			"--model", a.Model, "--no-tools", "--mode", "json", prompt)
		cmd.Env = os.Environ()
		out, err := cmd.Output()
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		content, cerr := extractPiFinalContent(lines)
		if content != "" {
			return content, nil
		}
		lastErr = cerr
	}
	return "", lastErr
}

// emitProgressPi streams pi's text-delta events to stderr so the human can watch in real time.
func emitProgressPi(line string) {
	var m struct {
		Type                  string `json:"type"`
		AssistantMessageEvent *struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		} `json:"assistantMessageEvent"`
	}
	if json.Unmarshal([]byte(line), &m) != nil {
		return
	}
	if m.Type == "message_update" && m.AssistantMessageEvent != nil &&
		m.AssistantMessageEvent.Type == "text_delta" {
		fmt.Fprint(os.Stderr, m.AssistantMessageEvent.Delta)
	}
}

// extractPiFinalContent reads pi's NDJSON stream. Pi uses a richer event schema than tau —
// the final content lives in "agent_end".messages (last assistant entry) or, failing that,
// "turn_end".message.content[0].text.
func extractPiFinalContent(lines []string) (string, error) {
	type contentItem struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type piMessage struct {
		Role    string        `json:"role"`
		Content []contentItem `json:"content"`
	}

	extractText := func(msgs []piMessage) string {
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role != "assistant" {
				continue
			}
			var sb strings.Builder
			for _, c := range msgs[i].Content {
				if c.Type == "text" {
					sb.WriteString(c.Text)
				}
			}
			if sb.Len() > 0 {
				return sb.String()
			}
		}
		return ""
	}

	// Walk in reverse — agent_end is the most terminal event; take the first one we find.
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		var ev struct {
			Type     string      `json:"type"`
			Messages []piMessage `json:"messages"` // agent_end
			Message  *piMessage  `json:"message"`  // turn_end / message_end
		}
		if json.Unmarshal([]byte(t), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "agent_end":
			if s := extractText(ev.Messages); s != "" {
				return s, nil
			}
		case "turn_end", "message_end":
			if ev.Message != nil && ev.Message.Role == "assistant" {
				var sb strings.Builder
				for _, c := range ev.Message.Content {
					if c.Type == "text" {
						sb.WriteString(c.Text)
					}
				}
				if sb.Len() > 0 {
					return sb.String(), nil
				}
			}
		}
	}
	return "", fmt.Errorf("no output from pi")
}
