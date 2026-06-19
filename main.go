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
	case "status":
		err = cmdStatus(os.Args[2:])
	case "answer":
		err = cmdAnswer(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
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
  mago init [dir]                 scaffold a local company (.mago/, STATE.md, tasks/, workspace/)
  mago task add "<title>" [--project <p>] [-C d]  create a task (optionally for a project)
  mago project add <name> [-C d]  register a project repo/workspace
  mago run <agent> [-C d]         run ONE tick: brief -> tau -> reflect -> write back
  mago loop <agent> [-C d]        run ticks on an adaptive cadence (--base/--max/--max-ticks secs)
  mago tick [-C d]                route open tasks to best-fit agents, then run each agent
  mago status [-C d]              show STATE.md, tasks, and pending HITL
  mago answer <id> "<text>" [-C d]  answer a needs_human task so it resumes
  mago version | help

Flags:
  -C <dir>     company directory (default: cwd, or $MAGO_COMPANY)

Env overrides (smoke test):
  MAGO_PROVIDER, MAGO_MODEL    override the agent's tau provider/model

The worker drives tau (stateless per tick) in <company>/workspace. Memory lives in
files: STATE.md (world), tasks/ (task), .mago/skills/ (lessons), .mago/runs/ (journals).
`)
}
