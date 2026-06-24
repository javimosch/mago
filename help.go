package main

// wantsHelp reports whether a subcommand's args request focused usage via -h/--help.
func wantsHelp(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// commandHelp holds focused per-command usage text, keyed by the top-level command.
// It is printed (to stdout, exit 0) when a known command is given -h/--help, so users
// who mistype a command's args get that command's synopsis instead of the full usage
// dump or a one-line error. Keep keys in sync with the dispatch switch in main().
const companyFlag = "\nFlags:\n  -C <dir>     company directory (default: cwd, or $MAGO_COMPANY)\n"

var commandHelp = map[string]string{
	"init": `mago init [dir]

Scaffold a local company in dir (default: cwd): .mago/, STATE.md, VISION.md,
ROADMAP.md, tasks/, workspace/, and the starter executive team.
`,
	"task": `mago task add "<title>" [--project <p>] [-C dir]

Create a task in the company backlog. With --project <p> the task is scoped to a
registered project repo.
` + companyFlag,
	"project": `mago project <subcommand> [-C dir]

  mago project add <name> --repo owner/repo [--mirror]   register a project repo
  mago project add owner/repo                            shorthand (infers name)
  mago project list                                      list projects + repos

--mirror also mirrors the company task as a GitHub issue.
` + companyFlag,
	"run": `mago run <agent> [-C dir]

Run ONE tick for <agent>: brief -> tau -> reflect -> write back.
` + companyFlag,
	"loop": `mago loop [<agent>] [-C dir]

Run ticks on an adaptive cadence: the interval resets to --base after work and
doubles (up to --max) when idle. With no agent it loops the full reconcile.

Flags:
  --base <secs>       base interval after work (default 3)
  --max <secs>        max idle interval (default 60)
  --max-ticks <n>     stop after n ticks (default 5)
  -C <dir>            company directory (default: cwd, or $MAGO_COMPANY)
`,
	"tick": `mago tick [-C dir]

Route open tasks to best-fit agents, then run each agent once.
` + companyFlag,
	"serve": `mago serve [-C dir]

Event-driven worker: GitHub webhooks wake a reconcile.

  mago serve stop|status      control/inspect a --daemon worker

Flags:
  --addr <addr>        listen address (default :8099)
  --secret <hmac>      GitHub webhook HMAC secret (or $MAGO_WEBHOOK_SECRET)
  --heartbeat <secs>   periodic reconcile interval
  --relay              dial out to the platform instead of a tunnel
  --daemon, -d         detach a supervisor (restart on crash; pidfile+log)
  --until HH:MM        stop cleanly at that wall-clock time
  --start-delay <dur>  wait before serving (fleet staggering, e.g. 15m)
  -C <dir>             company directory (default: cwd, or $MAGO_COMPANY)
`,
	"status": `mago status [-C dir]

Show STATE.md, the task list, and any pending human input (HITL).
` + companyFlag,
	"answer": `mago answer <task-id> "<text>" [-C dir]

Answer a needs_human task so it resumes on the next ` + "`mago run`" + `.
` + companyFlag,
	"digest": `mago digest [-C dir]

"What your company did": backlog, PRs, HITL, and today's autonomy budget.
` + companyFlag,
	"skills": `mago skills [<name>]

Print the embedded operator guide (operating, cli, fleet). With <name>, print
that one skill. The guide is current with this binary.
`,
	"mode": `mago mode [show | <tokens>] [-C dir]

Show or switch a LOCAL worker's mode live (no restart; applied within ~30s).
Tokens: reactive | proactive[=secs] | review | verified | comms=on|off.
` + companyFlag,
	"register": `mago register [--email <e>] [--password <p>]

Create a platform account; the token is saved to ~/.mago/config.json.
$MAGO_PASSWORD provides a non-interactive password.
`,
	"login": `mago login [--email <e>] [--password <p>]

Log in to an existing platform account; the token is saved to ~/.mago/config.json.
$MAGO_PASSWORD provides a non-interactive password.
`,
	"subscribe": `mago subscribe

Print the Stripe checkout link (€20/month).
`,
	"billing": `mago billing

Print the Stripe customer-portal link (manage/cancel the subscription).
`,
	"account": `mago account status

Show your plan and license key.
`,
	"link": `mago link --installation <id>

Claim a GitHub App installation (entitles your repos).

  mago link list      show linked installations + entitled repos
`,
	"worker": `mago worker <subcommand>

  mago worker mode <tokens> --worker <id>|--all   switch a REMOTE worker's mode
  mago worker doctor                              validate gh auth and the configured LLM harness (tau or claude, per MAGO_PROVIDER)

Mode tokens: reactive | proactive[=secs] | review | verified | comms=on|off.
`,
}
