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
	dir, rest, err := parseCompanyDir(args)
	if err != nil {
		return err
	}
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
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	agent := ""
	if len(pos) >= 1 {
		agent = pos[0]
	}

	interval := base
	for i := 0; i < maxTicks; i++ {
		var worked bool
		if agent == "" {
			worked, err = reconcileOnce(comp)
		} else {
			var res tickResult
			res, err = runTick(comp, agent)
			worked = res.worked
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "[loop] tick %d error: %v\n", i+1, err)
		}
		if err == nil && worked {
			interval = base
		} else {
			interval *= 2
			if interval > maxI {
				interval = maxI
			}
		}
		next := "(stop)"
		if i < maxTicks-1 {
			next = strconv.Itoa(interval) + "s"
		}
		fmt.Fprintf(os.Stderr, "[loop] tick %d: worked=%v -> next in %s\n", i+1, worked, next)
		if i < maxTicks-1 {
			time.Sleep(time.Duration(interval) * time.Second)
		}
	}
	return nil
}
