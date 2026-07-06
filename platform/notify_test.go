package main

import (
	"strings"
	"testing"
	"time"
)

func TestFormatMessage(t *testing.T) {
	cases := []struct {
		kind, email, detail string
		wantEmoji           rune
	}{
		{"signup", "alice@example.com", "with one-click login", '🆕'},
		{"subscribed", "bob@example.com", "→ mago €20/mo", '✅'},
		{"canceled", "charlie@example.com", "→ free", '❌'},
		{"worker_connect", "dave@example.com", "worker-1 · repos=...", '🟢'},
	}

	for _, c := range cases {
		msg := formatMessage(c.kind, c.email, c.detail)
		if !strings.HasPrefix(msg, string(c.wantEmoji)) {
			t.Errorf("formatMessage(%q) = %q, want emoji %q first", c.kind, msg, string(c.wantEmoji))
		}
		if !strings.Contains(msg, c.email) {
			t.Errorf("formatMessage(%q) = %q, want email %q", c.kind, msg, c.email)
		}
		if !strings.Contains(msg, c.detail) {
			t.Errorf("formatMessage(%q) = %q, want detail %q", c.kind, msg, c.detail)
		}
	}
}

func TestFormatMessageNoEmail(t *testing.T) {
	msg := formatMessage("signup", "", "with one-click login")
	if !strings.Contains(msg, "uid") {
		t.Errorf("formatMessage with empty email should use 'uid', got: %q", msg)
	}
}

func TestIsFirstWorkerConnect(t *testing.T) {
	// Reset the cache for this test.
	firstConnectMu.Lock()
	firstConnectSince = map[int64]time.Time{}
	firstConnectMu.Unlock()

	uid := int64(12345)

	// First call should return true.
	if !isFirstWorkerConnect(uid) {
		t.Error("first isFirstWorkerConnect should return true")
	}

	// Second call within the hour should return false.
	if isFirstWorkerConnect(uid) {
		t.Error("second isFirstWorkerConnect should return false (within grace period)")
	}

	// Manually set the time to >1 hour ago and test again.
	firstConnectMu.Lock()
	firstConnectSince[uid] = time.Now().Add(-2 * time.Hour)
	firstConnectMu.Unlock()

	if !isFirstWorkerConnect(uid) {
		t.Error("isFirstWorkerConnect after 1+ hour should return true")
	}
}
