package main

import (
	"fmt"
	"os"
)

const version = "0.0.1-poc"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(0)
	}
	var err error
	switch cmd := os.Args[1]; cmd {
	case "init":
		err = cmdInit(os.Args[2:])
	case "task":
		err = cmdTask(os.Args[2:])
	case "project":
		err = cmdProject(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "loop":
		err = cmdLoop(os.Args[2:])
	case "tick":
		err = cmdTick(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "answer":
		err = cmdAnswer(os.Args[2:])
	case "digest":
		err = cmdDigest(os.Args[2:])
	case "skills":
		err = cmdSkills(os.Args[2:])
	case "mode":
		err = cmdMode(os.Args[2:])
	case "register":
		err = cmdRegister(os.Args[2:])
	case "login":
		err = cmdLogin(os.Args[2:])
	case "subscribe":
		err = cmdSubscribe(os.Args[2:])
	case "billing":
		err = cmdBilling(os.Args[2:])
	case "account":
		err = cmdAccount(os.Args[2:])
	case "link":
		err = cmdLink(os.Args[2:])
	case "worker":
		err = cmdWorker(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "mago: unknown command %q\n", cmd)
		if s := suggestCommand(cmd); s != "" {
			fmt.Fprintf(os.Stderr, "\nDid you mean %q?\n", s)
		}
		fmt.Fprintln(os.Stderr, "\nRun 'mago help' to see all commands.")
		os.Exit(80)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`mago ` + version + ` — local POC: autonomy-level-3 agents over a local company

Usage:
  Account (platform):
  mago register [--email <e>] [--password <p>]   create an account (token -> ~/.mago/config.json)
  mago login [--email <e>] [--password <p>]      log in to an existing account
  mago subscribe                  print the Stripe checkout link (€20/month)
  mago billing                    print the Stripe customer-portal link (manage/cancel)
  mago account status             show plan + license key
  mago link --installation <id>   claim a GitHub App installation (entitles your repos)
  mago link list                  show linked installations + entitled repos

  Company (local/worker):
  mago init [dir]                 scaffold a local company (.mago/, STATE.md, tasks/, workspace/)
  mago task add "<title>" [--project <p>] [-C d]  create a task (optionally for a project)
  mago project add <name> --repo owner/repo [-C d]   register a project repo (or: add owner/repo)
  mago project list [-C d]        list registered projects + repos
  mago run <agent> [-C d]         run ONE tick: brief -> tau -> reflect -> write back
  mago loop <agent> [-C d]        run ticks on an adaptive cadence (--base/--max/--max-ticks secs)
  mago tick [-C d]                route open tasks to best-fit agents, then run each agent
  mago serve [-C d]               event-driven worker: GitHub webhooks wake a reconcile
                                  (--addr :8099, --secret <hmac>, --heartbeat <secs>,
                                   --relay = dial out to the platform instead of a tunnel,
                                   --daemon = detach a supervisor (restart on crash; pidfile+log),
                                   --until HH:MM = stop cleanly at that time,
                                   --start-delay <dur> = wait before serving (fleet staggering))
  mago serve stop|status [-C d]   control/inspect a --daemon worker
  mago mode [show | <tokens>] [-C d]  switch a LOCAL worker's mode live (reactive|proactive|verified|comms=on…)
  mago worker mode <tokens> --worker <id>|--all   switch a REMOTE worker's mode over the relay
  mago status [-C d]              show STATE.md, tasks, and pending HITL
  mago digest [-C d]              "what your company did": backlog, PRs, HITL, autonomy budget
  mago answer <id> "<text>" [-C d]  answer a needs_human task so it resumes
  mago worker doctor               validate tau, gh, and OPENCODE_API_KEY (exits 101 on failure)
  mago skills [<name>]             embedded operator guide (operating, cli, fleet) — current with this binary
  mago version | help

Flags:
  -C <dir>     company directory (default: cwd, or $MAGO_COMPANY)

Env overrides (smoke test):
  MAGO_COMPANY                 company directory (same as -C; default: cwd)
  MAGO_GH_REPO                 backlog repo "owner/repo" -> use GitHub-backed mode
                               (tasks=issues, status=labels); unset = local backend
  MAGO_TASK_LABEL              issue label marking a task in GitHub-backed mode
  MAGO_PROVIDER, MAGO_MODEL    override the agent's tau provider/model
  MAGO_PLATFORM_URL            platform API base (default http://localhost:9100)
  MAGO_PASSWORD                non-interactive password for register/login

The worker drives tau (stateless per tick) in <company>/workspace. Memory lives in
files: STATE.md (world), tasks/ (task), .mago/skills/ (lessons), .mago/runs/ (journals).
`)
}
