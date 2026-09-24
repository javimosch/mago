package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ratelimit.go is a small per-IP fixed-window limiter for the endpoints that an anonymous
// visitor can reach and that cost us something to serve.
//
// /auth/portier/start is the one that matters: portier bills 1 EUR per 100 successful auths
// past a free tier, and an app whose wallet goes past_due has NEW logins blocked. So an
// unauthenticated start endpoint is not just a cost — it is a denial-of-signup, where the
// attacker spends our money to close our front door. Limiting it is load-bearing.

// clientIP resolves the caller's address.
//
// The platform runs behind Traefik on the same host, so the TCP peer is loopback and the real
// address is in X-Forwarded-For. That header is attacker-controlled in general, so it is only
// trusted when the connection actually came from the local proxy — otherwise a direct caller
// could mint a fresh identity per request and make the limiter decorative.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return host // direct connection: the peer address is the only trustworthy one
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Leftmost entry is the original client as recorded by our own proxy.
		if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
			return first
		}
	}
	return host
}

type rateWindow struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string]*rateWindow
	limit  int
	window time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: map[string]*rateWindow{}, limit: limit, window: window}
}

// allow records a hit and reports whether it is within the window's budget.
func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()

	// Opportunistic sweep: without it the map grows with every distinct address seen. Cheap
	// because it only runs when the map is already large.
	if len(rl.hits) > 4096 {
		for k, w := range rl.hits {
			if now.After(w.reset) {
				delete(rl.hits, k)
			}
		}
	}

	w, ok := rl.hits[key]
	if !ok || now.After(w.reset) {
		rl.hits[key] = &rateWindow{count: 1, reset: now.Add(rl.window)}
		return true
	}
	w.count++
	return w.count <= rl.limit
}

// Budgets. Deliberately generous for a human and tight for a script: a real person signs up
// once, and retries a handful of times at worst.
var (
	// Each allowed start can become a billable portier auth.
	signupLimiter = newRateLimiter(10, time.Hour)
	// Guessing guard. The code space is 32^8, so brute force is hopeless anyway; this stops
	// the noise and keeps a wrong-code loop from hammering the DB.
	claimLimiter = newRateLimiter(20, time.Hour)
)

// limited wraps a handler with a per-IP budget. On refusal it answers 429 with Retry-After,
// so a well-behaved client backs off instead of spinning.
func limited(rl *rateLimiter, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "3600")
			httpErr(w, http.StatusTooManyRequests, "too many attempts from this address — try again later")
			return
		}
		h(w, r)
	}
}
