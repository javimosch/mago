package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// feedback.go implements the cli-feedback-spec convention.
// Dual-writes: (1) the mago platform endpoint (best-effort), (2) the central relay
// at feedback.intrane.fr (best-effort). Same id on both writes = idempotent.
// The command MUST NOT fail the caller — a failed write is reported, not raised.

const defaultRelayURL = "https://feedback.intrane.fr"

// parseFeedbackArgs splits the args into the free-text message and an optional --kind.
func parseFeedbackArgs(args []string) (msg, kind string) {
	kind = "note"
	var words []string
	for i := 0; i < len(args); i++ {
		if (args[i] == "--kind" || args[i] == "-k" || args[i] == "--type" || args[i] == "-t") && i+1 < len(args) {
			kind = args[i+1]
			i++
			continue
		}
		if args[i] == "--context" && i+1 < len(args) {
			i++
			continue
		}
		words = append(words, args[i])
	}
	return strings.TrimSpace(strings.Join(words, " ")), kind
}

func parseContext(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--context" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func genFeedbackID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func reporter() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "agent"
}

// postFeedback sends a feedback submission to a URL. Returns true on success.
func postFeedback(url string, body map[string]any) bool {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func cmdFeedback(args []string) error {
	msg, kind := parseFeedbackArgs(args)
	if msg == "" {
		return fmt.Errorf("usage: mago feedback \"<message>\" [--kind bug|idea|praise|note] [--context \"<what you were doing>\"]")
	}
	ctx := parseContext(args)
	id := genFeedbackID()

	body := map[string]any{
		"id":       id,
		"app":      "mago",
		"message":  msg,
		"kind":     kind,
		"version":  version,
		"context":  ctx,
		"reporter": reporter(),
	}

	// Write 1: platform endpoint (best-effort, may need auth)
	stored := 0
	cfg := loadConfig()
	platformBody := map[string]any{
		"message":  msg,
		"type":     kind, // platform still uses "type"
		"version":  version,
		"context":  ctx,
		"reporter": reporter(),
		"id":       id,
	}
	var platformOut struct {
		OK       bool   `json:"ok"`
		IssueURL string `json:"issue_url"`
	}
	if err := cfg.platformDo("POST", "/api/feedback", platformBody, true, &platformOut); err == nil {
		stored = 1
	}

	// Write 2: relay (best-effort, honor FEEDBACK_RELAY=off)
	relayed := 0
	relayURL := os.Getenv("FEEDBACK_RELAY")
	if relayURL == "" {
		relayURL = defaultRelayURL
	}
	if relayURL != "off" {
		if postFeedback(relayURL+"/v1/feedback", body) {
			relayed = 1
		}
	}

	// Never fail the caller
	fmt.Printf("{\"ok\":true,\"id\":\"%s\",\"stored\":%d,\"relayed\":%d}\n", id, stored, relayed)
	if stored == 0 && relayed == 0 {
		fmt.Fprintln(os.Stderr, "warning: feedback could not be delivered, but was not lost — please retry later.")
	}
	return nil
}
