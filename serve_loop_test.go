package main

import (
	"testing"
	"time"
)

// TestHeartbeatLoop verifies the heartbeat ticker wakes the worker with a
// coalescable "heartbeat" event. A short ticker keeps the test fast.
func TestHeartbeatLoop(t *testing.T) {
	w := &eventWorker{comp: &Company{Name: "test"}, wake: make(chan wakeEvent, 4)}
	go w.heartbeatLoop(1 * time.Millisecond)

	select {
	case ev := <-w.wake:
		if ev.reason != "heartbeat" {
			t.Errorf("heartbeat reason = %q, want %q", ev.reason, "heartbeat")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("heartbeat event not received")
	}
}

// TestProactiveLoop verifies proactive planning wakes the worker with a
// "proactive cadence" event when the live mode has a positive cadence.
func TestProactiveLoop(t *testing.T) {
	c := newTestCompany(t)
	if err := c.saveMode(workerMode{Proactive: 1, Merge: "review"}); err != nil {
		t.Fatalf("saveMode: %v", err)
	}

	w := &eventWorker{comp: c, wake: make(chan wakeEvent, 4)}
	go w.proactiveLoop()

	select {
	case ev := <-w.wake:
		if ev.reason != "proactive cadence" {
			t.Errorf("proactive reason = %q, want %q", ev.reason, "proactive cadence")
		}
		if !ev.proactive {
			t.Error("proactive event not marked proactive")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proactive cadence event not received")
	}
}
