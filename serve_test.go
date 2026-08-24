package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
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

func TestWebhookSecret(t *testing.T) {
	t.Run("env value is trimmed", func(t *testing.T) {
		t.Setenv("MAGO_WEBHOOK_SECRET", "  shhh  ")
		if got := webhookSecret(); got != "shhh" {
			t.Errorf("webhookSecret() = %q, want %q", got, "shhh")
		}
	})

	t.Run("empty env returns empty", func(t *testing.T) {
		t.Setenv("MAGO_WEBHOOK_SECRET", "")
		if got := webhookSecret(); got != "" {
			t.Errorf("webhookSecret() = %q, want empty", got)
		}
	})
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

func TestHandleWebhook_NoSecret(t *testing.T) {
	w := &eventWorker{wake: make(chan wakeEvent, 4)}
	h := w.handleWebhook("")

	body := `{"action":"opened","issue":{"number":42}}`
	req := httptest.NewRequest("POST", "/webhook/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), "wake=true") {
		t.Errorf("response = %q, want wake=true", rr.Body.String())
	}
	if len(w.wake) != 1 {
		t.Fatalf("want 1 queued wake, got %d", len(w.wake))
	}
	ev := <-w.wake
	if ev.reason != "issue #42 opened" {
		t.Errorf("reason = %q, want %q", ev.reason, "issue #42 opened")
	}
}

// TestCmdServe_StatusStopped verifies cmdServe "status" prints "stopped" when
// no worker is running for the company.
func TestCmdServe_StatusStopped(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)

	out := captureStdout(t, func() {
		if err := cmdServe([]string{"-C", c.Dir, "status"}); err != nil {
			t.Fatalf("cmdServe status: %v", err)
		}
	})
	if !strings.Contains(out, "stopped") {
		t.Errorf("output = %q, want 'stopped'", out)
	}
}

// TestCmdServe_StopNoWorker verifies cmdServe "stop" errors cleanly when no
// worker is running for the company.
func TestCmdServe_StopNoWorker(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)

	err := cmdServe([]string{"-C", c.Dir, "stop"})
	if err == nil {
		t.Fatal("cmdServe stop with no worker should error")
	}
	if !strings.Contains(err.Error(), "no worker running") {
		t.Errorf("error = %q, want 'no worker running'", err.Error())
	}
}

// TestCmdServe_InvalidStartDelay verifies cmdServe rejects a malformed --start-delay
// before starting the worker. This keeps fleet-stagger typos from silently degrading
// to a 0s delay and then binding a listener.
func TestCmdServe_InvalidStartDelay(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)

	err := cmdServe([]string{"-C", c.Dir, "--start-delay", "not-a-duration"})
	if err == nil {
		t.Fatal("cmdServe with invalid --start-delay should error")
	}
	if !strings.Contains(err.Error(), "must be a duration") {
		t.Errorf("error = %q, want 'must be a duration'", err.Error())
	}
}

// TestCmdServe_InvalidUntil verifies cmdServe rejects a malformed --until value
// before it starts the event loop or binds a listener.
func TestCmdServe_InvalidUntil(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)

	err := cmdServe([]string{"-C", c.Dir, "--until", "bad"})
	if err == nil {
		t.Fatal("cmdServe with invalid --until should error")
	}
	if !strings.Contains(err.Error(), "HH:MM") {
		t.Errorf("error = %q, want 'HH:MM'", err.Error())
	}
}

// TestEventWorkerRun_PausesAtBudgetCap verifies eventWorker.run continues when
// the daily budget cap is hit, skipping the heavy work (propose/review/tick/reconcile)
// without panicking or hanging.
func TestEventWorkerRun_PausesAtBudgetCap(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_DAILY_BUDGET", "1")
	c := newTestCompany(t)
	c.saveUsage(budgetUsage{Day: utcDay(), Actions: 1})

	w := &eventWorker{comp: c, wake: make(chan wakeEvent, 4)}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.run()
	}()

	w.wake <- wakeEvent{reason: "proactive cadence", proactive: true}
	w.wake <- wakeEvent{reason: "PR merged", prRepo: "acme/repo", prNum: 1, prTitle: "t", comms: true}
	w.wake <- wakeEvent{reason: "human comment on #1", target: "foo"}
	w.wake <- wakeEvent{reason: "startup"}
	w.wake <- wakeEvent{reason: "heartbeat"}
	close(w.wake)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("eventWorker.run did not return after the wake channel closed")
	}
}

// TestHeartbeatLoop_Serve verifies that heartbeatLoop emits periodic "heartbeat" wake events
// on a short ticker without overflowing the queue.
func TestHeartbeatLoop_Serve(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)
	w := &eventWorker{comp: c, wake: make(chan wakeEvent, 8)}

	go w.heartbeatLoop(50 * time.Millisecond)
	time.Sleep(130 * time.Millisecond)

	if got := len(w.wake); got < 2 {
		t.Errorf("got %d heartbeat events, want at least 2", got)
	}
}

// TestProactiveLoop_Serve verifies that proactiveLoop emits a "proactive cadence" wake event
// when the company mode has a positive proactive cadence.
func TestProactiveLoop_Serve(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)
	if err := c.saveMode(workerMode{Proactive: 1, Merge: "on"}); err != nil {
		t.Fatalf("saveMode: %v", err)
	}

	w := &eventWorker{comp: c, wake: make(chan wakeEvent, 8)}
	go w.proactiveLoop()

	select {
	case ev := <-w.wake:
		if ev.reason != "proactive cadence" || !ev.proactive {
			t.Errorf("unexpected wake event: %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proactiveLoop did not emit a proactive cadence event")
	}
}
