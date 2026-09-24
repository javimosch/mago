package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// typedError prints a cli-output-spec conformant error and exits.
func typedError(code int, errType, message string, recoverable bool, suggestions []string) {
	rec := false
	if recoverable {
		rec = true
	}
	if suggestions == nil {
		suggestions = []string{}
	}
	body := map[string]any{
		"error": map[string]any{
			"code":        code,
			"type":        errType,
			"message":     message,
			"recoverable": rec,
			"suggestions": suggestions,
		},
	}
	out, _ := json.Marshal(body)
	fmt.Println(string(out))
	os.Exit(code)
}

// cmdHelpJSON prints the machine-readable command catalog.
func cmdHelpJSON(args []string) error {
	catalog := map[string]any{
		"name":        "mago",
		"description": "Local POC: autonomy-level-3 agents over a local company",
		"binary":      "mago",
		"version":     version,
		"commands": []map[string]string{
			{"name": "help-json", "description": "Print the machine-readable command catalog"},
			{"name": "guide", "description": "Print the agent guide (JSON or --human markdown)"},
			{"name": "version", "description": "Print version info"},
			{"name": "init", "description": "Scaffold a local company"},
			{"name": "task", "description": "Create a task in the backlog"},
			{"name": "project", "description": "Register/list project repos"},
			{"name": "run", "description": "Run ONE tick: brief -> tau -> reflect -> write back"},
			{"name": "loop", "description": "Run ticks on adaptive cadence"},
			{"name": "tick", "description": "Route open tasks to best-fit agents, then run"},
			{"name": "serve", "description": "Event-driven worker: GitHub webhooks wake a reconcile"},
			{"name": "daemon", "description": "Daemon lifecycle: start|stop|status over /_health and /_shutdown"},
			{"name": "status", "description": "Show STATE.md, tasks, and pending HITL"},
			{"name": "digest", "description": "What your company did: backlog, PRs, HITL, autonomy budget"},
			{"name": "answer", "description": "Answer a needs_human task so it resumes"},
			{"name": "skills", "description": "Embedded operator guide"},
			{"name": "mode", "description": "Switch a LOCAL worker's mode live"},
			{"name": "feedback", "description": "Report friction/bugs/requests to the mago team"},
			{"name": "worker", "description": "Worker management (doctor, mode)"},
			{"name": "update", "description": "Self-update this binary to the platform's latest release (--check, --force)"},
			{"name": "install", "description": "Copy this binary into <prefix>/mago (default ~/.local/bin)"},
			{"name": "uninstall", "description": "Remove the installed binary (no-op if absent)"},
			{"name": "claim", "description": "Attach this machine to an account created in the browser at /signup"},
		},
		"exit_codes": map[string]string{
			"0":   "success",
			"5":   "update --check: an update is available (not an error)",
			"80":  "input_error",
			"85":  "invalid_argument",
			"90":  "resource_not_found",
			"100": "upstream_error",
			"110": "internal_error",
		},
	}
	out, _ := json.Marshal(catalog)
	fmt.Println(string(out))
	return nil
}

// cmdGuide prints the agent guide as JSON (default) or markdown (--human).
func cmdGuide(args []string) error {
	human := false
	for _, a := range args {
		if a == "--human" || a == "-h" {
			human = true
		}
	}
	if human {
		printGuideHuman()
		return nil
	}
	guide := map[string]any{
		"version":   version,
		"one_liner": "Local POC: autonomy-level-3 agents over a local company — tick, loop, serve",
		"model":     "Mago runs autonomous agents over a local company directory. Each tick: brief the agent on state, run tau (stateless LLM call), reflect on the result, write back to STATE.md and task files. Agents are stateless per tick; memory lives in files (STATE.md, tasks/, .mago/skills/, .mago/runs/). The company is a directory with .mago/ config, STATE.md (world), tasks/ (task state), and workspace/ (agent working dir). GitHub-backed mode uses issues as tasks and labels as status.",
		"loop": []string{
			"mago init [dir]              # scaffold a local company",
			"mago task add \"<title>\"       # create a task",
			"mago project add <name> --repo owner/repo  # register a repo",
			"mago tick [-C dir]           # route tasks to agents, run each",
			"mago run <agent> [-C dir]    # run ONE tick for a specific agent",
			"mago loop <agent> [-C dir]   # run ticks on adaptive cadence",
			"mago status [-C dir]         # see state + pending HITL",
			"mago digest [-C dir]         # what your company did",
		},
		"concepts": map[string]string{
			"company": "A directory with .mago/ config, STATE.md (world state), tasks/ (task files), workspace/ (agent working dir), and .mago/skills/ (lessons).",
			"tick":    "One reconciliation cycle: route open tasks to best-fit agents, run each agent (brief -> tau -> reflect -> write back).",
			"tau":     "The stateless LLM call per tick. Provider/model via MAGO_PROVIDER/MAGO_MODEL. Returns a structured response that gets written back.",
			"hitl":    "Human-in-the-loop: tasks that need human input are parked; `mago answer <id> \"<text>\"` resumes them.",
			"mode":    "Agent autonomy level: reactive, proactive, verified, comms. Switch live with `mago mode`.",
			"merge":   "Per-PR merge policy (mago mode merge=<value>): review (mago approves/comments, a HUMAN always merges -- default, safest), verified (auto-merges ONLY when the LLM approves AND a real build/test check passes -- set MAGO_VERIFY_CMD or rely on auto-detect for known stacks), on (auto-merges on LLM approval alone, no build/test gate -- highest autonomy, least safety net). verified is a STRICTER superset of on, not a step below it: it does everything `on` does plus requires a passing check first.",
			"github":  "GitHub-backed mode: MAGO_GH_REPO=owner/repo uses issues as tasks, labels as status. Webhook-driven via `mago serve`.",
			"serve":   "Event-driven worker: GitHub webhooks wake a reconcile instead of polling. --daemon detaches a supervisor.",
			"daemon":  "Daemon lifecycle per cli-daemon-spec: start|stop|status, idempotent, over /_health and /_shutdown.",
		},
		"commands": map[string]string{
			"help-json": "Print the machine-readable command catalog",
			"guide":     "Print this agent guide (JSON or --human markdown)",
			"version":   "Print version info",
			"init":      "Scaffold a local company",
			"task":      "Create/list tasks",
			"project":   "Register/list project repos",
			"run":       "Run ONE tick for a specific agent",
			"loop":      "Run ticks on adaptive cadence",
			"tick":      "Route open tasks to best-fit agents, then run",
			"serve":     "Event-driven worker (webhooks)",
			"daemon":    "Daemon lifecycle (start|stop|status)",
			"status":    "Show state + pending HITL",
			"digest":    "What your company did",
			"answer":    "Answer a needs_human task",
			"skills":    "Embedded operator guide",
			"mode":      "Switch agent mode",
			"feedback":  "Report friction/bugs",
			"worker":    "Worker management",
			"update":    "Self-update this binary (--check exits 5 when an update is available)",
			"install":   "Copy this binary into <prefix>/mago (default ~/.local/bin)",
			"uninstall": "Remove the installed binary",
			"claim":     "Attach this machine to a browser-created account",
		},
		"examples": []string{
			"mago init mycompany  # scaffold",
			"mago task add \"Fix login bug\"  # create a task",
			"mago tick  # route + run all agents once",
			"mago run claude  # run one tick for claude agent",
			"mago status  # see what happened",
			"mago digest  # morning summary",
		},
		"gotchas": []string{
			"Agents are stateless per tick — all memory is in files (STATE.md, tasks/), not in-process",
			"Company dir defaults to cwd; use -C <dir> or MAGO_COMPANY to override",
			"GitHub-backed mode needs MAGO_GH_REPO + gh auth; local mode needs nothing",
			"serve --daemon detaches a supervisor with pidfile+log; use `mago serve stop` to stop",
			"merge=on is LESS safe than merge=verified, not more autonomous in a good way -- on skips the build/test check entirely and merges on the LLM's opinion alone. verified still auto-merges (same autonomy) but only after a real build/test passes -- prefer verified over on once you have a working MAGO_VERIFY_CMD.",
			"verify.go clones the PR branch into a SEPARATE dir (.mago/verify/<repo>), never the live workspace/ -- a build failure there reflects the PR/base branch, not whatever devin/tau is mid-editing in workspace/ right now.",
			"A PR review (and its verification) only fires on webhook actions opened/reopened/ready_for_review -- NOT on synchronize (a new commit) and NOT retroactively for already-open PRs when you change merge/verify config. Close+reopen a PR to force a fresh pass.",
			"This is a POC (0.0.2) — not production-ready",
		},
	}
	out, _ := json.Marshal(guide)
	fmt.Println(string(out))
	return nil
}

func printGuideHuman() {
	fmt.Print(`# mago — agent guide

Local POC: autonomy-level-3 agents over a local company.

## Model

Mago runs autonomous agents over a local company directory. Each tick:
brief the agent on state, run tau (stateless LLM call), reflect on the
result, write back to STATE.md and task files.

## Loop

  mago init [dir]              # scaffold a local company
  mago task add "<title>"       # create a task
  mago tick [-C dir]           # route tasks to agents, run each
  mago run <agent> [-C dir]    # run ONE tick for a specific agent
  mago status [-C dir]         # see state + pending HITL
  mago digest [-C dir]         # what your company did

## Commands

  help-json    Print the machine-readable command catalog
  guide        Print this guide (JSON or --human markdown)
  version      Print version info
  tick         Route + run all agents once
  run          Run one tick for a specific agent
  loop         Run ticks on adaptive cadence
  serve        Event-driven worker (webhooks)
  status       Show state + pending HITL
  digest       What your company did
  update       Self-update this binary (--check, --force)
  install      Copy this binary into <prefix>/mago (default ~/.local/bin)
  uninstall    Remove the installed binary

## Merge modes (mago mode merge=<value>)

- review    mago approves/comments, a HUMAN always merges (default, safest)
- verified  auto-merges ONLY when the LLM approves AND a real build/test
            check passes (set MAGO_VERIFY_CMD, or auto-detect for known stacks)
- on        auto-merges on LLM approval alone, no build/test gate

verified is a STRICTER superset of on, not a step below it.

## Gotchas

- Agents are stateless per tick — all memory is in files
- Company dir defaults to cwd; use -C <dir> or MAGO_COMPANY
- merge=on is LESS safe than merge=verified, not more autonomous in a good way
- A PR review only fires on opened/reopened/ready_for_review — never
  retroactively when you change merge/verify config; close+reopen to force one
- This is a POC (0.0.2) — not production-ready
`)
}
