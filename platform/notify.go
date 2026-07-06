package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// notifyOnEvent sends a Telegram message for high-signal onboarding events.
// Runs fire-and-forget in a goroutine with a short timeout to avoid blocking.
func notifyOnEvent(kind string, uid int64, email, detail string) {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if botToken == "" || chatID == "" {
		return // Telegram not configured; skip.
	}

	// Only notify on high-signal events (and de-dupe worker_connect).
	switch kind {
	case "signup", "subscribed", "canceled":
		// Always notify for these events.
	case "worker_connect":
		// Only notify on first connect per account; de-dupe subsequent connects within a grace period.
		if !isFirstWorkerConnect(uid) {
			return
		}
	default:
		return // Not a high-signal event.
	}

	// Fire-and-forget to avoid blocking the request hot path.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		sendTelegramMessage(ctx, botToken, chatID, formatMessage(kind, email, detail))
	}()
}

// isFirstWorkerConnect tracks whether a given account has connected before, returning true
// only on the first connect (or first connect after the grace period). Uses a simple in-memory
// cache to de-dupe rapid reconnects from the same worker.
func isFirstWorkerConnect(uid int64) bool {
	firstConnectMu.Lock()
	defer firstConnectMu.Unlock()

	ts, seen := firstConnectSince[uid]
	now := time.Now()

	// If we haven't seen this uid or it's been >1 hour since we last connected, notify.
	if !seen || now.Sub(ts) > 1*time.Hour {
		firstConnectSince[uid] = now
		return true
	}
	return false
}

var (
	firstConnectMu    sync.Mutex
	firstConnectSince = map[int64]time.Time{}
)

// sendTelegramMessage sends a message to the Telegram chat via the Bot API.
func sendTelegramMessage(ctx context.Context, botToken, chatID, message string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	payload := map[string]string{
		"chat_id": chatID,
		"text":    message,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram: HTTP %d", resp.StatusCode)
	}
	return nil
}

// formatMessage creates a concise, emoji-prefixed Telegram notification.
func formatMessage(kind, email, detail string) string {
	emoji := map[string]string{
		"signup":      "🆕",
		"subscribed":  "✅",
		"canceled":    "❌",
		"worker_connect": "🟢",
	}[kind]

	if email == "" {
		email = "uid"
	}
	return fmt.Sprintf("%s %s: %s", emoji, email, detail)
}
