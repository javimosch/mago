package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestValidSignature(t *testing.T) {
	secret := "shhh"
	body := []byte("payload")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !validSignature(secret, want, body) {
		t.Errorf("validSignature(%q, %q, %q) = false, want true", secret, want, body)
	}

	// Wrong secret produces a different mac.
	if validSignature("other", want, body) {
		t.Errorf("validSignature with wrong secret should fail")
	}

	// Missing or malformed prefix is rejected.
	if validSignature(secret, hex.EncodeToString(mac.Sum(nil)), body) {
		t.Errorf("validSignature without sha256= prefix should fail")
	}
	if validSignature(secret, "sha1=deadbeef", body) {
		t.Errorf("validSignature with sha1 prefix should fail")
	}
	if validSignature(secret, "", body) {
		t.Errorf("validSignature with empty signature should fail")
	}
}

func TestUntilDuration(t *testing.T) {
	// Valid HH:MM → a duration in (0, 24h].
	d, err := untilDuration("09:00")
	if err != nil {
		t.Fatalf("untilDuration(09:00): %v", err)
	}
	if d <= 0 || d > 24*time.Hour {
		t.Errorf("duration %s out of (0,24h]", d)
	}
	// A time one minute from now is ~today (well under 24h), not pushed to tomorrow.
	soon := time.Now().Add(time.Minute).Format("15:04")
	if d, _ := untilDuration(soon); d > 23*time.Hour {
		t.Errorf("near-future %s should be ~today, got %s", soon, d)
	}
	// Invalid input errors.
	for _, bad := range []string{"25:00", "9am", "", "09:99"} {
		if _, err := untilDuration(bad); err == nil {
			t.Errorf("untilDuration(%q) should error", bad)
		}
	}
}

func TestClassifyEvent(t *testing.T) {
	// For labeled events, a custom task label can also wake the worker.
	t.Setenv("MAGO_TASK_LABEL", "task")

	tests := []struct {
		name       string
		event      string
		body       string
		wantOK     bool
		wantReason string
		wantTarget string
	}{
		{
			name:       "issue opened",
			event:      "issues",
			body:       `{"action":"opened","issue":{"number":42}}`,
			wantOK:     true,
			wantReason: "issue #42 opened",
		},
		{
			name:       "issue labeled with control label",
			event:      "issues",
			body:       `{"action":"labeled","issue":{"number":7},"label":{"name":"mago:go"}}`,
			wantOK:     true,
			wantReason: "issue #7 labeled mago:go",
		},
		{
			name:   "issue labeled with ignored label",
			event:  "issues",
			body:   `{"action":"labeled","issue":{"number":7},"label":{"name":"nope"}}`,
			wantOK: false,
		},
		{
			name:       "issue labeled with custom task label",
			event:      "issues",
			body:       `{"action":"labeled","issue":{"number":8},"label":{"name":"task"}}`,
			wantOK:     true,
			wantReason: "issue #8 labeled task",
		},
		{
			name:       "human comment wakes owner",
			event:      "issue_comment",
			body:       `{"action":"created","issue":{"number":3,"labels":[{"name":"agent:foo"}]},"comment":{"body":"hello"}}`,
			wantOK:     true,
			wantReason: "human comment on #3",
			wantTarget: "foo",
		},
		{
			name:   "mago comment is ignored",
			event:  "issue_comment",
			body:   `{"action":"created","issue":{"number":3},"comment":{"body":"🔧 fixed"}}`,
			wantOK: false,
		},
		{
			name:       "PR opened",
			event:      "pull_request",
			body:       `{"action":"opened","number":9,"pull_request":{"number":9,"title":"t","head":{"ref":"feature"}},"repository":{"full_name":"acme/corp"}}`,
			wantOK:     true,
			wantReason: "PR #9 opened",
		},
		{
			name:   "PR closed but not merged",
			event:  "pull_request",
			body:   `{"action":"closed","pull_request":{"merged":false,"head":{"ref":"feature"}}}`,
			wantOK: false,
		},
		{
			name:       "PR merged on mago/task branch wakes comms",
			event:      "pull_request",
			body:       `{"action":"closed","pull_request":{"number":10,"title":"done","merged":true,"head":{"ref":"mago/task-1"}},"repository":{"full_name":"acme/corp"}}`,
			wantOK:     true,
			wantReason: "PR #10 merged",
		},
		{
			name:   "unknown event",
			event:  "fork",
			body:   `{}`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// default empty env so MAGO_TASK_LABEL does not leak across unrelated cases
			if tt.name != "issue labeled with custom task label" {
				os.Unsetenv("MAGO_TASK_LABEL")
			} else {
				os.Setenv("MAGO_TASK_LABEL", "task")
			}
			got, ok := classifyEvent(tt.event, []byte(tt.body))
			if ok != tt.wantOK {
				t.Fatalf("classifyEvent ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", got.reason, tt.wantReason)
			}
			if got.target != tt.wantTarget {
				t.Errorf("target = %q, want %q", got.target, tt.wantTarget)
			}
		})
	}
}

func TestSignal(t *testing.T) {
	w := &eventWorker{wake: make(chan wakeEvent, 4)}

	// Non-coalescable events always queue.
	w.signal(wakeEvent{reason: "human comment on #1"})
	if got := len(w.wake); got != 1 {
		t.Fatalf("want 1 queued, got %d", got)
	}

	// Fill the channel to capacity.
	for i := 0; i < 3; i++ {
		w.signal(wakeEvent{reason: fmt.Sprintf("issue #%d", i)})
	}
	if got := len(w.wake); got != 4 {
		t.Fatalf("want channel full at 4, got %d", got)
	}

	// One-shot events block on a full channel and are dropped.
	w.signal(wakeEvent{reason: "dropped one-shot"})
	if got := len(w.wake); got != 4 {
		t.Errorf("one-shot should be dropped when full, got %d queued", got)
	}

	// Coalescable (heartbeat/proactive) events are dropped once the queue is half full.
	w2 := &eventWorker{wake: make(chan wakeEvent, 4)}
	w2.signal(wakeEvent{reason: "heartbeat", proactive: false})
	w2.signal(wakeEvent{reason: "heartbeat", proactive: false})
	w2.signal(wakeEvent{reason: "heartbeat", proactive: false})
	if got := len(w2.wake); got != 3 {
		t.Fatalf("want 3 heartbeats queued, got %d", got)
	}
	w2.signal(wakeEvent{reason: "heartbeat", proactive: false}) // > cap/2, should be dropped
	if got := len(w2.wake); got != 3 {
		t.Errorf("coalescable should be dropped past half full, got %d queued", got)
	}
}
