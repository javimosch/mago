package main

import (
	"context"
	"errors"
	"io"
	"net/http"
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

// TestNotifyOnEvent_WhitespaceToken verifies whitespace-only Telegram env values
// are treated as unconfigured, so the worker-connect cache is not touched.
func TestNotifyOnEvent_WhitespaceToken(t *testing.T) {
	firstConnectMu.Lock()
	firstConnectSince = map[int64]time.Time{}
	firstConnectMu.Unlock()

	t.Setenv("TELEGRAM_BOT_TOKEN", "   ")
	t.Setenv("TELEGRAM_CHAT_ID", "\t\n")

	notifyOnEvent("worker_connect", 3, "c@example.com", "worker-2")

	firstConnectMu.Lock()
	defer firstConnectMu.Unlock()
	if len(firstConnectSince) != 0 {
		t.Errorf("firstConnectSince should remain empty with whitespace token, got %d entries", len(firstConnectSince))
	}
}

// TestNotifyOnEvent_FiresHighSignal verifies that a configured Telegram
// client receives a well-formed request for high-signal events.
func TestNotifyOnEvent_FiresHighSignal(t *testing.T) {
	firstConnectMu.Lock()
	firstConnectSince = map[int64]time.Time{}
	firstConnectMu.Unlock()

	t.Setenv("TELEGRAM_BOT_TOKEN", "bot-token")
	t.Setenv("TELEGRAM_CHAT_ID", "chat123")

	orig := http.DefaultClient
	defer func() { http.DefaultClient = orig }()

	reqCh := make(chan *http.Request, 1)
	http.DefaultClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		reqCh <- req
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
	})}

	notifyOnEvent("signup", 1, "a@example.com", "one-click login")

	select {
	case req := <-reqCh:
		if req.URL.Host != "api.telegram.org" {
			t.Errorf("host = %q, want api.telegram.org", req.URL.Host)
		}
		if got, want := req.URL.Path, "/botbot-token/sendMessage"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		_ = req.Body.Close()
		s := string(body)
		if !strings.Contains(s, "chat123") || !strings.Contains(s, "one-click login") {
			t.Errorf("body missing chat or detail: %s", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no Telegram request received")
	}
}

// TestNotifyOnEvent_WorkerConnectDedupe verifies that repeated worker_connect
// events within the grace period only fire one Telegram notification.
func TestNotifyOnEvent_WorkerConnectDedupe(t *testing.T) {
	firstConnectMu.Lock()
	firstConnectSince = map[int64]time.Time{}
	firstConnectMu.Unlock()

	t.Setenv("TELEGRAM_BOT_TOKEN", "bot-token")
	t.Setenv("TELEGRAM_CHAT_ID", "chat123")

	orig := http.DefaultClient
	defer func() { http.DefaultClient = orig }()

	reqCh := make(chan *http.Request, 2)
	http.DefaultClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		reqCh <- req
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
	})}

	uid := int64(42)
	notifyOnEvent("worker_connect", uid, "w@example.com", "worker-1")
	notifyOnEvent("worker_connect", uid, "w@example.com", "worker-1 again")

	var count int
	done := time.After(500 * time.Millisecond)
loop:
	for {
		select {
		case <-reqCh:
			count++
		case <-done:
			break loop
		}
	}

	if count != 1 {
		t.Errorf("got %d worker_connect notifications, want 1", count)
	}
}

// TestNotifyOnEvent_Unconfigured verifies that when Telegram is not configured
// the function returns before doing any work and does not touch the worker-connect cache.
func TestNotifyOnEvent_Unconfigured(t *testing.T) {
	firstConnectMu.Lock()
	firstConnectSince = map[int64]time.Time{}
	firstConnectMu.Unlock()

	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")

	notifyOnEvent("signup", 1, "a@example.com", "one-click login")
	notifyOnEvent("worker_connect", 2, "b@example.com", "worker-1")

	firstConnectMu.Lock()
	defer firstConnectMu.Unlock()
	if len(firstConnectSince) != 0 {
		t.Errorf("firstConnectSince should remain empty, got %d entries", len(firstConnectSince))
	}
}

// TestNotifyOnEvent_UnknownKind verifies that an unknown event kind with Telegram
// configured returns before sending any HTTP request and does not touch the
// worker-connect cache.
func TestNotifyOnEvent_UnknownKind(t *testing.T) {
	firstConnectMu.Lock()
	firstConnectSince = map[int64]time.Time{}
	firstConnectMu.Unlock()

	t.Setenv("TELEGRAM_BOT_TOKEN", "bot-token")
	t.Setenv("TELEGRAM_CHAT_ID", "chat123")

	orig := http.DefaultClient
	defer func() { http.DefaultClient = orig }()

	reqCh := make(chan *http.Request, 1)
	http.DefaultClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		reqCh <- req
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
	})}

	notifyOnEvent("unknown_kind", 7, "u@example.com", "should not fire")

	select {
	case <-reqCh:
		t.Fatal("unknown event kind should not send a Telegram request")
	case <-time.After(200 * time.Millisecond):
		// expected
	}

	firstConnectMu.Lock()
	defer firstConnectMu.Unlock()
	if len(firstConnectSince) != 0 {
		t.Errorf("firstConnectSince should remain empty for unknown kind, got %d entries", len(firstConnectSince))
	}
}

// roundTripperFunc is a simple http.RoundTripper adapter for tests.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestSendTelegramMessage verifies the Telegram POST is well-formed and HTTP
// errors and transport failures are surfaced.
func TestSendTelegramMessage(t *testing.T) {
	orig := http.DefaultClient
	defer func() { http.DefaultClient = orig }()

	t.Run("success", func(t *testing.T) {
		http.DefaultClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != "POST" {
				t.Errorf("want POST, got %s", req.Method)
			}
			if req.URL.Host != "api.telegram.org" {
				t.Errorf("want host api.telegram.org, got %s", req.URL.Host)
			}
			if got, want := req.URL.Path, "/bottoken/sendMessage"; got != want {
				t.Errorf("path = %q, want %q", got, want)
			}

			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			_ = req.Body.Close()
			s := string(body)
			if !strings.Contains(s, "chat123") || !strings.Contains(s, "hello") {
				t.Errorf("body missing chat or text: %s", s)
			}

			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
		})}

		if err := sendTelegramMessage(context.Background(), "token", "chat123", "hello"); err != nil {
			t.Fatalf("sendTelegramMessage: %v", err)
		}
	})

	t.Run("http error", func(t *testing.T) {
		http.DefaultClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusInternalServerError, Body: http.NoBody, Request: req}, nil
		})}

		err := sendTelegramMessage(context.Background(), "token", "chat123", "hello")
		if err == nil {
			t.Fatal("expected error for 500 response")
		}
		if !strings.Contains(err.Error(), "telegram: HTTP 500") {
			t.Errorf("error = %q, want 'telegram: HTTP 500'", err.Error())
		}
	})

	t.Run("transport error", func(t *testing.T) {
		http.DefaultClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})}

		err := sendTelegramMessage(context.Background(), "token", "chat123", "hello")
		if err == nil {
			t.Fatal("expected error for transport failure")
		}
		if !strings.Contains(err.Error(), "network down") {
			t.Errorf("error = %q, want 'network down'", err.Error())
		}
	})
}
