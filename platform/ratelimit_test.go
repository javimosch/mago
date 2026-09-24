package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRateLimiterBudget: the window allows exactly `limit` hits, then refuses.
func TestRateLimiterBudget(t *testing.T) {
	rl := newRateLimiter(3, time.Hour)
	for i := 1; i <= 3; i++ {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("hit %d should be allowed", i)
		}
	}
	if rl.allow("1.2.3.4") {
		t.Error("the fourth hit should be refused")
	}
	if !rl.allow("5.6.7.8") {
		t.Error("a different address has its own budget")
	}
}

// TestRateLimiterWindowResets: the budget is per window, not per process lifetime.
func TestRateLimiterWindowResets(t *testing.T) {
	rl := newRateLimiter(1, 20*time.Millisecond)
	if !rl.allow("1.2.3.4") {
		t.Fatal("first hit should be allowed")
	}
	if rl.allow("1.2.3.4") {
		t.Fatal("second hit inside the window should be refused")
	}
	time.Sleep(30 * time.Millisecond)
	if !rl.allow("1.2.3.4") {
		t.Error("the budget should refresh once the window has passed")
	}
}

// TestClientIPIgnoresSpoofedForwardedFor is the one that matters. X-Forwarded-For is
// attacker-controlled, so it may only be believed when the connection came from our own
// loopback proxy. A direct caller sending the header must not be able to mint a fresh
// identity per request — that would make the limiter decorative.
func TestClientIPIgnoresSpoofedForwardedFor(t *testing.T) {
	direct := httptest.NewRequest("GET", "/", nil)
	direct.RemoteAddr = "203.0.113.9:5555"
	direct.Header.Set("X-Forwarded-For", "9.9.9.9")
	if got := clientIP(direct); got != "203.0.113.9" {
		t.Errorf("a direct caller's XFF must be ignored, got %q", got)
	}

	proxied := httptest.NewRequest("GET", "/", nil)
	proxied.RemoteAddr = "127.0.0.1:5555"
	proxied.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.1")
	if got := clientIP(proxied); got != "198.51.100.7" {
		t.Errorf("behind the local proxy the leftmost XFF entry wins, got %q", got)
	}

	bare := httptest.NewRequest("GET", "/", nil)
	bare.RemoteAddr = "127.0.0.1:5555"
	if got := clientIP(bare); got != "127.0.0.1" {
		t.Errorf("no XFF should fall back to the peer, got %q", got)
	}
}

// TestLimitedHandlerAnswers429: a refused request is a typed 429 with Retry-After, not a
// silent drop, so a well-behaved client backs off instead of spinning.
func TestLimitedHandlerAnswers429(t *testing.T) {
	calls := 0
	h := limited(newRateLimiter(1, time.Hour), func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(200)
	})

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = "203.0.113.5:1111"
		h(rec, req)
		if i == 0 && rec.Code != 200 {
			t.Fatalf("first request should pass, got %d", rec.Code)
		}
		if i == 1 {
			if rec.Code != http.StatusTooManyRequests {
				t.Errorf("second request should be 429, got %d", rec.Code)
			}
			if rec.Header().Get("Retry-After") == "" {
				t.Error("a 429 should carry Retry-After")
			}
		}
	}
	if calls != 1 {
		t.Errorf("the wrapped handler should have run once, ran %d times", calls)
	}
}

// TestSignupStartIsRateLimited: the billable endpoint is actually wrapped, not merely
// wrappable. Each allowed start can become a paid portier auth, and exhausting the wallet
// blocks new logins — so this is a denial-of-signup guard, not just cost control.
func TestSignupStartIsRateLimited(t *testing.T) {
	t.Setenv("MAGO_PORTIER_APP_ID", "app_test")
	t.Setenv("MAGO_PORTIER_SECRET", "psk_test")
	s := newTestServer(t)

	rl := newRateLimiter(2, time.Hour)
	h := limited(rl, s.handlePortierStart)

	var last int
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/auth/portier/start?provider=github", nil)
		req.RemoteAddr = "203.0.113.20:2222"
		h(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("start should be refused past its budget, got %d", last)
	}
}
