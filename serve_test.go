package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

// TestProactiveLoop_WakesOnCadence verifies that the proactive loop reads the cadence from the
// live mode, sleeps for that duration, and then signals a proactive wake event so the worker
// picks up the planning task. It uses an injectable sleep so the test does not wait on wall clock.
func TestProactiveLoop_WakesOnCadence(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "co"}
	c.saveMode(workerMode{Proactive: 1, Merge: "review"})

	w := &eventWorker{comp: c, wake: make(chan wakeEvent, 4)}

	sleepCh := make(chan time.Duration, 2)
	proceed := make(chan struct{})
	w.sleep = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		sleepCh <- d
		select {
		case <-proceed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.proactiveLoop(ctx)

	// Wait for the first cadence sleep to be requested.
	var d time.Duration
	select {
	case d = <-sleepCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for proactive loop to request sleep")
	}
	if d != time.Second {
		t.Errorf("sleep duration = %v, want 1s", d)
	}

	// Allow the sleep to elapse; the worker should then emit a proactive wake.
	proceed <- struct{}{}
	select {
	case ev := <-w.wake:
		if !ev.proactive {
			t.Errorf("ev.proactive = %v, want true", ev.proactive)
		}
		if ev.reason != "proactive cadence" {
			t.Errorf("ev.reason = %q, want %q", ev.reason, "proactive cadence")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for proactive wake event")
	}
}

// TestProactiveLoop_IdlesWhenReactive verifies that when the live mode is reactive (Proactive=0)
// the loop does not emit proactive wake events; it polls the mode on the 30s idle cadence.
func TestProactiveLoop_IdlesWhenReactive(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "co"}
	c.saveMode(workerMode{Proactive: 0, Merge: "review"})

	w := &eventWorker{comp: c, wake: make(chan wakeEvent, 4)}

	sleepCh := make(chan time.Duration, 2)
	ctx, cancel := context.WithCancel(context.Background())
	w.sleep = func(ctx context.Context, d time.Duration) error {
		sleepCh <- d
		<-ctx.Done()
		return ctx.Err()
	}

	go w.proactiveLoop(ctx)

	var d time.Duration
	select {
	case d = <-sleepCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for reactive idle sleep")
	}
	if d != 30*time.Second {
		t.Errorf("reactive idle sleep duration = %v, want 30s", d)
	}

	// No proactive wake should be emitted in reactive mode.
	select {
	case ev := <-w.wake:
		t.Fatalf("unexpected proactive wake while reactive: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
}

// TestProactiveLoop_RespectsLiveModeSwitch verifies that the loop re-reads the live mode after
// the sleep elapses, so a proactive->reactive switch while it is sleeping suppresses the wake.
func TestProactiveLoop_RespectsLiveModeSwitch(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "co"}
	c.saveMode(workerMode{Proactive: 1, Merge: "review"})

	w := &eventWorker{comp: c, wake: make(chan wakeEvent, 4)}

	sleepCh := make(chan time.Duration, 2)
	proceed := make(chan struct{})
	w.sleep = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		sleepCh <- d
		select {
		case <-proceed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.proactiveLoop(ctx)

	// Wait for the proactive sleep.
	select {
	case d := <-sleepCh:
		if d != time.Second {
			t.Errorf("sleep duration = %v, want 1s", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for proactive sleep")
	}

	// Switch the live mode to reactive before the sleep elapses.
	c.saveMode(workerMode{Proactive: 0, Merge: "review"})
	proceed <- struct{}{}

	// No proactive wake should be emitted after the mode switched to reactive.
	select {
	case ev := <-w.wake:
		t.Fatalf("unexpected proactive wake after reactive switch: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}

	// The loop should now idle at the reactive poll cadence.
	select {
	case d := <-sleepCh:
		if d != 30*time.Second {
			t.Errorf("reactive idle sleep duration = %v, want 30s", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for reactive idle sleep")
	}
}

// TestProactiveLoop_RespectsReactiveSwitchOnLongCadence verifies that a long proactive cadence is
// not a single uninterruptible sleep; a switch to reactive is picked up at the next 30s poll.
func TestProactiveLoop_RespectsReactiveSwitchOnLongCadence(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "co"}
	c.saveMode(workerMode{Proactive: 100, Merge: "review"})

	w := &eventWorker{comp: c, wake: make(chan wakeEvent, 4)}

	sleepCh := make(chan time.Duration, 2)
	proceed := make(chan struct{})
	w.sleep = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		sleepCh <- d
		select {
		case <-proceed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.proactiveLoop(ctx)

	// Wait for the first 30s chunk of the long proactive cadence.
	select {
	case d := <-sleepCh:
		if d != 30*time.Second {
			t.Errorf("long-cadence chunk = %v, want 30s", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first long-cadence chunk")
	}

	// Switch to reactive mid-cadence.
	c.saveMode(workerMode{Proactive: 0, Merge: "review"})
	proceed <- struct{}{}

	// No proactive wake should be emitted, and the loop should fall back to idle.
	select {
	case ev := <-w.wake:
		t.Fatalf("unexpected proactive wake on long cadence after reactive switch: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}

	select {
	case d := <-sleepCh:
		if d != 30*time.Second {
			t.Errorf("reactive idle sleep duration = %v, want 30s", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for reactive idle sleep")
	}
}
