package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// nextInterval is the heart of the adaptive cadence: reset on productive ticks,
// exponential backoff (with --max clamping) otherwise.
func TestNextInterval(t *testing.T) {
	tests := []struct {
		name            string
		cur, base, maxI int
		worked          bool
		tickErr         error
		want            int
	}{
		{"reset on work", 48, 3, 60, true, nil, 3},
		{"reset from base when work", 3, 3, 60, true, nil, 3},
		{"backoff when idle", 3, 3, 60, false, nil, 6},
		{"backoff doubles again", 6, 3, 60, false, nil, 12},
		{"clamp at max", 40, 3, 60, false, nil, 60},
		{"already at max stays clamped", 60, 3, 60, false, nil, 60},
		{"error counts as not worked despite worked=true", 3, 3, 60, true, errors.New("boom"), 6},
		{"error backs off even when idle", 12, 3, 60, false, errors.New("boom"), 24},
		{"work wins only without error", 30, 3, 60, true, nil, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextInterval(tt.cur, tt.base, tt.maxI, tt.worked, tt.tickErr)
			if got != tt.want {
				t.Fatalf("nextInterval(%d,%d,%d,%v,%v) = %d, want %d",
					tt.cur, tt.base, tt.maxI, tt.worked, tt.tickErr, got, tt.want)
			}
		})
	}
}

func TestParseLoopArgs_Defaults(t *testing.T) {
	base, maxI, maxTicks, pos := parseLoopArgs(nil)
	if base != 3 || maxI != 60 || maxTicks != 5 {
		t.Fatalf("defaults = base %d, max %d, maxTicks %d; want 3/60/5", base, maxI, maxTicks)
	}
	if len(pos) != 0 {
		t.Fatalf("expected no positional args, got %v", pos)
	}
}

func TestParseLoopArgs_Flags(t *testing.T) {
	base, maxI, maxTicks, pos := parseLoopArgs([]string{
		"--base", "5", "cto", "--max", "120", "--max-ticks", "10",
	})
	if base != 5 {
		t.Errorf("base = %d, want 5", base)
	}
	if maxI != 120 {
		t.Errorf("max = %d, want 120", maxI)
	}
	if maxTicks != 10 {
		t.Errorf("maxTicks = %d, want 10", maxTicks)
	}
	if !reflect.DeepEqual(pos, []string{"cto"}) {
		t.Errorf("pos = %v, want [cto]", pos)
	}
}

func TestParseLoopArgs_DanglingFlagKeepsDefault(t *testing.T) {
	// A flag with no following value is ignored, leaving the default in place.
	base, _, maxTicks, pos := parseLoopArgs([]string{"--base"})
	if base != 3 {
		t.Errorf("base = %d, want default 3 for dangling --base", base)
	}
	if maxTicks != 5 {
		t.Errorf("maxTicks = %d, want default 5", maxTicks)
	}
	if len(pos) != 0 {
		t.Errorf("pos = %v, want empty (flag consumed)", pos)
	}
}

// driveLoop must terminate after exactly maxTicks ticks and sleep between (but not
// after) them.
func TestDriveLoop_MaxTicksTermination(t *testing.T) {
	var runs, sleeps int
	ran := driveLoop(3, 60, 4,
		func(i int) (bool, error) { runs++; return false, nil },
		func(seconds int) { sleeps++ },
	)
	if ran != 4 {
		t.Errorf("driveLoop returned %d ticks, want 4", ran)
	}
	if runs != 4 {
		t.Errorf("run called %d times, want 4", runs)
	}
	if sleeps != 3 {
		t.Errorf("sleep called %d times, want 3 (no sleep after final tick)", sleeps)
	}
}

func TestDriveLoop_ZeroTicks(t *testing.T) {
	var runs int
	ran := driveLoop(3, 60, 0,
		func(i int) (bool, error) { runs++; return true, nil },
		func(seconds int) {},
	)
	if ran != 0 || runs != 0 {
		t.Fatalf("maxTicks=0 should run nothing: ran=%d runs=%d", ran, runs)
	}
}

// cmdLoop surfaces a loadCompany error (e.g. a directory with no .mago/) instead
// of proceeding into the tick loop.
func TestCmdLoop_LoadCompanyError(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	dir := t.TempDir() // no .mago/

	err := cmdLoop([]string{"-C", dir, "dev", "--base", "0", "--max-ticks", "1"})
	if err == nil {
		t.Fatal("expected error for missing .mago/")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error = %q, want 'not a mago company'", err.Error())
	}
}

// The full cadence over a run: idle ticks back off and clamp, a productive tick
// resets back to base.
func TestDriveLoop_CadenceSequence(t *testing.T) {
	// base 3, max 10. Worked outcomes per tick: idle, idle, idle, work, idle.
	worked := []bool{false, false, false, true, false}
	var intervals []int
	driveLoop(3, 10, len(worked),
		func(i int) (bool, error) { return worked[i], nil },
		func(seconds int) { intervals = append(intervals, seconds) },
	)
	// After t0 idle: 3->6. After t1 idle: 6->12 clamped to 10. After t2 idle:
	// 10->20 clamped to 10. After t3 work: reset to 3. (No sleep after final t4.)
	want := []int{6, 10, 10, 3}
	if !reflect.DeepEqual(intervals, want) {
		t.Fatalf("interval sequence = %v, want %v", intervals, want)
	}
}

// An errored tick backs off just like an idle one, even if it reported worked=true.
func TestDriveLoop_ErrorBacksOff(t *testing.T) {
	var intervals []int
	driveLoop(3, 60, 3,
		func(i int) (bool, error) { return true, errors.New("tick failed") },
		func(seconds int) { intervals = append(intervals, seconds) },
	)
	// t0 errored: 3->6. t1 errored: 6->12. (No sleep after final t2.)
	want := []int{6, 12}
	if !reflect.DeepEqual(intervals, want) {
		t.Fatalf("interval sequence = %v, want %v", intervals, want)
	}
}

// cmdLoop wires parseLoopArgs, loadCompany, and driveLoop together. A zero-base,
// single-tick run with an agent exercises the agent branch without real sleep.
func TestCmdLoop_AgentTick(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Developer\n---\n")

	if err := cmdLoop([]string{"-C", c.Dir, "dev", "--base", "0", "--max-ticks", "1"}); err != nil {
		t.Fatalf("cmdLoop error: %v", err)
	}
}

// cmdLoop surfaces a parseCompanyDir error (e.g. a bare -C with no directory) before
// it tries to load the company or enter the tick loop.
func TestCmdLoop_ParseCompanyDirError(t *testing.T) {
	err := cmdLoop([]string{"-C"})
	if err == nil {
		t.Fatal("expected error for bare -C")
	}
	if !strings.Contains(err.Error(), "-C needs a directory value") {
		t.Errorf("error = %q, want '-C needs a directory value'", err.Error())
	}
}

// With no agent argument, cmdLoop's tick runs the full reconcileOnce path. On a
// company with an empty agents dir reconcileOnce reports "no agents" as a tick
// error, which driveLoop logs and backs off from — the loop still returns nil.
// A zero base keeps the real time.Sleep between ticks instant.
func TestCmdLoop_NoAgentReconcileError(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t) // .mago/agents exists but is empty

	if err := cmdLoop([]string{"-C", c.Dir, "--base", "0", "--max-ticks", "2"}); err != nil {
		t.Fatalf("cmdLoop error: %v", err)
	}
}
