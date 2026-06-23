package main

import (
	"fmt"
	"runtime"
	"strings"
)

// feedback.go lets an operator — a human, or (more often) the AI agent driving mago — report
// friction, bugs, or feature requests straight from the CLI. It POSTs to the platform, which records
// it and files a triage item (a GitHub issue labeled `feedback`, NOT auto-implemented). Because mago
// is agent-operated, the operator can self-report the moment a command is confusing or blocks — the
// richest usage signal there is, available from the very first interaction.

// parseFeedbackArgs splits the args into the free-text message and an optional --type.
func parseFeedbackArgs(args []string) (msg, ftype string) {
	ftype = "feedback"
	var words []string
	for i := 0; i < len(args); i++ {
		if (args[i] == "--type" || args[i] == "-t") && i+1 < len(args) {
			ftype = args[i+1]
			i++
			continue
		}
		words = append(words, args[i])
	}
	return strings.TrimSpace(strings.Join(words, " ")), ftype
}

func cmdFeedback(args []string) error {
	msg, ftype := parseFeedbackArgs(args)
	if msg == "" {
		return fmt.Errorf("usage: mago feedback \"<what didn't click / a bug / a feature request>\" " +
			"[--type bug|friction|feature|question]")
	}
	cfg := loadConfig()
	body := map[string]any{
		"message": msg,
		"type":    ftype,
		"version": version,
		"os":      runtime.GOOS + "/" + runtime.GOARCH,
	}
	var out struct {
		OK       bool   `json:"ok"`
		IssueURL string `json:"issue_url"`
	}
	if err := cfg.platformDo("POST", "/api/feedback", body, true, &out); err != nil {
		return err
	}
	fmt.Println("thanks — your feedback reached the mago team.")
	if out.IssueURL != "" {
		fmt.Printf("  tracked at: %s\n", out.IssueURL)
	}
	return nil
}
