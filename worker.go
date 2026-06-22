package main

import (
	"fmt"
	"os"
	"os/exec"
)

func cmdWorker(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: mago worker <subcommand>")
		fmt.Fprintln(os.Stderr, "  subcommands: doctor")
		os.Exit(80)
	}
	switch args[0] {
	case "doctor":
		workerDoctor()
	default:
		fmt.Fprintf(os.Stderr, "unknown worker subcommand: %s\n", args[0])
		os.Exit(80)
	}
	return nil
}

type diagCheck struct {
	label string
	ok    bool
	hint  string
}

// workerDoctor validates that tau, gh, and OPENCODE_API_KEY are configured.
// Prints a pass/fail line per check with a fix hint on failure.
// Exits 101 if any check fails (integration error per AGENTS.md exit code map).
func workerDoctor() {
	checks := []diagCheck{
		checkTau(),
		checkGhOnPath(),
		checkGhAuth(),
		checkOpenCodeAPIKey(),
	}

	failed := 0
	for _, c := range checks {
		if c.ok {
			fmt.Printf("  [ok]   %s\n", c.label)
		} else {
			fmt.Printf("  [fail] %s\n         fix: %s\n", c.label, c.hint)
			failed++
		}
	}

	fmt.Println()
	if failed > 0 {
		fmt.Printf("%d check(s) failed — fix the above and re-run `mago worker doctor`\n", failed)
		os.Exit(101)
	}
	fmt.Println("All checks passed. Worker is ready.")
}

func checkTau() diagCheck {
	if _, err := exec.LookPath("tau"); err != nil {
		return diagCheck{
			label: "tau (LLM driver) on PATH",
			ok:    false,
			hint:  "install tau from https://opencode.ai then ensure it is on your PATH",
		}
	}
	return diagCheck{label: "tau (LLM driver) on PATH", ok: true}
}

func checkGhOnPath() diagCheck {
	if _, err := exec.LookPath("gh"); err != nil {
		return diagCheck{
			label: "gh (GitHub CLI) on PATH",
			ok:    false,
			hint:  "install gh from https://cli.github.com then re-run",
		}
	}
	return diagCheck{label: "gh (GitHub CLI) on PATH", ok: true}
}

func checkGhAuth() diagCheck {
	if _, err := exec.LookPath("gh"); err != nil {
		// gh not installed — auth check is moot; already reported above
		return diagCheck{
			label: "gh authenticated",
			ok:    false,
			hint:  "install gh first, then run: gh auth login",
		}
	}
	cmd := exec.Command("gh", "auth", "status")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return diagCheck{
			label: "gh authenticated",
			ok:    false,
			hint:  "run: gh auth login",
		}
	}
	return diagCheck{label: "gh authenticated", ok: true}
}

func checkOpenCodeAPIKey() diagCheck {
	if os.Getenv("OPENCODE_API_KEY") == "" {
		return diagCheck{
			label: "OPENCODE_API_KEY set",
			ok:    false,
			hint:  "export OPENCODE_API_KEY=<your-key>  (get one at opencode.ai)",
		}
	}
	return diagCheck{label: "OPENCODE_API_KEY set", ok: true}
}
