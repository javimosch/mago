package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// cmdLoop runs an agent on an adaptive cadence: the interval resets to --base when
// the agent did real work, and doubles (up to --max) when it was idle. This is the
// POC's cheapness dial — an idle company backs off and costs ~nothing.
func cmdLoop(args []string) error {
	dir, rest := parseCompanyDir(args)
	base, maxI, maxTicks := 3, 60, 5
	var pos []string
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
	if len(pos) < 1 {
		return fmt.Errorf("usage: mago loop <agent> [--base secs] [--max secs] [--max-ticks n] [-C dir]")
	}
	agent := pos[0]
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}

	interval := base
	for i := 0; i < maxTicks; i++ {
		res, err := runTick(comp, agent)
		sig := "error"
		if err == nil {
			sig = orDefault(res.signal, "idle")
		} else {
			fmt.Fprintf(os.Stderr, "[loop] tick %d error: %v\n", i+1, err)
		}
		if err == nil && res.worked && res.signal != "idle" {
			interval = base // active: stay responsive
		} else {
			interval *= 2 // idle/blocked/error: back off
			if interval > maxI {
				interval = maxI
			}
		}
		next := "(stop)"
		if i < maxTicks-1 {
			next = strconv.Itoa(interval) + "s"
		}
		fmt.Fprintf(os.Stderr, "[loop] tick %d: worked=%v signal=%s -> next in %s\n",
			i+1, err == nil && res.worked, sig, next)
		if i < maxTicks-1 {
			time.Sleep(time.Duration(interval) * time.Second)
		}
	}
	return nil
}
