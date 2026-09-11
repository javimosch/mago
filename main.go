package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

const version = "0.0.2-poc"

// cliErr is an error that carries a semantic exit code per AGENTS.md's exit-code table
// (80–89 user errors, 90–99 resource errors, 100–109 integration errors). cmd* functions
// return it so main() can propagate the right code; uncategorized errors exit 110
// (software/internal) — never a bare 1, which sits outside the table agents branch on.
type cliErr struct {
	code int
	msg  string
}

func (e *cliErr) Error() string { return e.msg }

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(0)
	}
	cmd := os.Args[1]
	// A known command given -h/--help prints its focused usage and exits, so a
	// mistyped invocation gets that command's synopsis rather than a one-line error.
	if h, ok := commandHelp[cmd]; ok && wantsHelp(os.Args[2:]) {
		fmt.Print(h)
		os.Exit(0)
	}
	var err error
	switch cmd {
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
	case "daemon":
		err = cmdDaemon(os.Args[2:])
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
	case "feedback":
		err = cmdFeedback(os.Args[2:])
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
		jsonFlag := false
		for _, a := range os.Args[2:] {
			if a == "--json" {
				jsonFlag = true
			}
		}
		if jsonFlag {
			out, _ := json.Marshal(map[string]string{"name": "mago", "version": version})
			fmt.Println(string(out))
		} else {
			fmt.Println(version)
		}
	case "help-json":
		err = cmdHelpJSON(os.Args[2:])
	case "guide":
		err = cmdGuide(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		sugg := []string{"Run: mago help"}
		if s := suggestCommand(cmd); s != "" {
			sugg = append([]string{fmt.Sprintf("Did you mean: mago %s", s)}, sugg...)
		}
		typedError(85, "invalid_argument", fmt.Sprintf("unknown command %q", cmd), false, sugg)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitCodeFor(err))
	}
}

// exitCodeFor resolves the semantic exit code for a command error: a *cliErr's
// code wins even when the error was wrapped, and anything else is an unexpected
// software error (110 per AGENTS.md's table).
func exitCodeFor(err error) int {
	var ce *cliErr
	if errors.As(err, &ce) {
		return ce.code
	}
	return 110
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
  mago worker doctor               validate gh auth and the configured LLM harness (tau or claude per MAGO_PROVIDER; exits 101 on failure)
  mago skills [<name>]             embedded operator guide (operating, cli, fleet) — current with this binary
  mago feedback "<msg>" [--type bug|friction|feature|question]   report friction/bugs/requests to the mago team
  mago version | help

Flags:
  -C <dir>     company directory (default: cwd, or $MAGO_COMPANY)

Env overrides (smoke test):
  MAGO_COMPANY                 company directory (same as -C; default: cwd)
  MAGO_GH_REPO                 backlog repo "owner/repo" -> use GitHub-backed mode
                               (tasks=issues, status=labels); unset = local backend
  MAGO_GH_TOKEN                Personal Access Token (repo scope) for GitHub API calls
                               in GitHub-backed mode; create one at
                               https://github.com/settings/tokens?type=legacy
  MAGO_TASK_LABEL              issue label marking a task in GitHub-backed mode
  MAGO_PROVIDER, MAGO_MODEL    override the agent's tau provider/model
  MAGO_NO_MERGE=1              reviewer comments but never auto-merges
  MAGO_VERIFY=1                auto-detect and run verification before approval
  MAGO_VERIFY_CMD              explicit verification command (e.g. "go test ./...")
  MAGO_MERGE_UNVERIFIED=1      auto-merge when no verification check is detected
  MAGO_UPDATE=auto             self-update the worker binary on a new platform release
  MAGO_DAILY_BUDGET=<n>        max work cycles per day (0 = unlimited)
  MAGO_PROACTIVE=<secs>        proactive planning cadence (unset = off)
  MAGO_PROACTIVE_MAX=<n>       max new issues per planning cycle (default 2)
  MAGO_PR_CAP=<n>              stop starting work after this many open mago PRs
  MAGO_ISSUE_CAP=<n>           stop proposing backlog after this many open issues
  MAGO_COMMS=1                 enable beyond-code deliverables when a PR merges
  MAGO_STATE_SYNC=1            push state to mago-state branch in MAGO_GH_REPO
  MAGO_PLATFORM_URL            platform API base (default http://localhost:9100)
  MAGO_WORKER_ID               worker identity for relay registration (default: hostname)
  MAGO_WEBHOOK_SECRET          GitHub webhook HMAC secret for mago serve (also --secret)
  MAGO_PASSWORD                non-interactive password for register/login

The worker drives tau (stateless per tick) in <company>/workspace. Memory lives in
files: STATE.md (world), tasks/ (task), .mago/skills/ (lessons), .mago/runs/ (journals).
`)
}
