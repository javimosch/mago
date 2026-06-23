package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// cmdLoop runs ticks on an adaptive cadence: the interval resets to --base when work
// happened, and doubles (up to --max) when idle. With no agent argument it loops the
// full reconcile (route + all agents) — an unattended company; with an agent it loops
// just that agent. This is the POC's cheapness dial.
func cmdLoop(args []string) error {
	dir, rest := parseCompanyDir(args)
	base, maxI, maxTicks, pos := parseLoopArgs(rest)
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	agent := ""
	if len(pos) >= 1 {
		agent = pos[0]
	}

	driveLoop(base, maxI, maxTicks,
		func(i int) (bool, error) {
			if agent == "" {
				return reconcileOnce(comp)
			}
			res, err := runTick(comp, agent)
			return res.worked, err
		},
		func(seconds int) { time.Sleep(time.Duration(seconds) * time.Second) },
	)
	return nil
}

// parseLoopArgs reads the loop flags (--base, --max, --max-ticks) out of rest,
// applying the defaults (3s base, 60s max, 5 ticks), and returns the remaining
// positional arguments (the optional agent name).
func parseLoopArgs(rest []string) (base, maxI, maxTicks int, pos []string) {
	base, maxI, maxTicks = 3, 60, 5
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--base":
			if i+1 < len(rest) {
				base = atoiSafe(rest[i+1])
				i++
			}
		case "--max":
			if i+1 < len(rest) {
				maxI = atoiSafe(rest[i+1])
				i++
			}
		case "--max-ticks":
			if i+1 < len(rest) {
				maxTicks = atoiSafe(rest[i+1])
				i++
			}
		default:
			pos = append(pos, rest[i])
		}
	}
	return
}

// nextInterval computes the wait before the next tick: reset to base when the tick
// did real work without erroring, otherwise back off exponentially (double the
// current interval) clamped to maxI.
func nextInterval(cur, base, maxI int, worked bool, tickErr error) int {
	if tickErr == nil && worked {
		return base
	}
	next := cur * 2
	if next > maxI {
		next = maxI
	}
	return next
}

// driveLoop runs up to maxTicks ticks on the adaptive cadence. run(i) performs the
// tick and reports whether work happened plus any error; sleep is called with the
// computed interval (seconds) between ticks but never after the final tick. It
// returns the number of ticks actually run (bounded by maxTicks).
func driveLoop(base, maxI, maxTicks int, run func(i int) (bool, error), sleep func(seconds int)) int {
	interval := base
	ticks := 0
	for i := 0; i < maxTicks; i++ {
		ticks++
		worked, err := run(i)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[loop] tick %d error: %v\n", i+1, err)
		}
		interval = nextInterval(interval, base, maxI, worked, err)
		next := "(stop)"
		if i < maxTicks-1 {
			next = strconv.Itoa(interval) + "s"
		}
		fmt.Fprintf(os.Stderr, "[loop] tick %d: worked=%v -> next in %s\n", i+1, worked, next)
		if i < maxTicks-1 {
			sleep(interval)
		}
	}
	return ticks
}
