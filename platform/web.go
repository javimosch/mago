package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// supportedPlatforms is the set of client builds published at /dl/mago (os-arch).
var supportedPlatforms = map[string]bool{
	"linux-amd64": true, "linux-arm64": true,
	"darwin-amd64": true, "darwin-arm64": true,
}

// web.go is the public front door served by the platform: a single-page landing, the CLI
// install script + binary, and the agent-first operator guide. mago is CLI-only — there is no
// web dashboard; this is marketing + the install path + onboarding docs.

func (s *server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" { // ServeMux "/" catches unmatched paths
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, landingHTML, s.appURL, s.appURL, s.appURL)
}

// handleInstall serves a POSIX sh installer that pulls the binary from this host.
func (s *server) handleInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	fmt.Fprintf(w, installScript, s.appURL)
}

// handleDownload serves the prebuilt mago client binary for the requested os/arch
// (?os=darwin&arch=arm64; defaults to linux/amd64). Binaries live in MAGO_CLI_DIR as
// mago-<os>-<arch>; MAGO_CLI_BINARY is the legacy single-file fallback for linux/amd64.
func (s *server) handleDownload(w http.ResponseWriter, r *http.Request) {
	osName := r.URL.Query().Get("os")
	if osName == "" {
		osName = "linux"
	}
	arch := r.URL.Query().Get("arch")
	if arch == "" {
		arch = "amd64"
	}
	plat := osName + "-" + arch
	if !supportedPlatforms[plat] {
		httpErr(w, 404, "unsupported platform "+plat+" (have: linux-amd64, linux-arm64, darwin-amd64, darwin-arm64)")
		return
	}
	bin := ""
	if dir := os.Getenv("MAGO_CLI_DIR"); dir != "" {
		bin = filepath.Join(dir, "mago-"+plat) // plat is allowlisted above — no traversal
	} else if plat == "linux-amd64" {
		bin = os.Getenv("MAGO_CLI_BINARY") // legacy single-binary fallback
	}
	if bin == "" {
		httpErr(w, 503, "cli binary not published for "+plat)
		return
	}
	f, err := os.Open(bin)
	if err != nil {
		httpErr(w, 404, "cli binary unavailable for "+plat)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="mago"`)
	io.Copy(w, f)
}

func (s *server) handleOperators(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, operatorsHTML, s.appURL, s.appURL)
}

// handleLLMs serves /llms.txt — the canonical agent-readable onboarding doc. mago is operated
// by an AI agent, so this is the "skill" an operator fetches on arrival: the exact CLI flow in
// plain markdown, no scraping required.
func (s *server) handleLLMs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, llmsText, s.appURL, s.appURL, s.appURL)
}

const css = `<style>
  :root{--fg:#1a1a2e;--mut:#555;--acc:#5319e7;--bg:#fafafe;--code:#f0f0f7}
  *{box-sizing:border-box} body{font-family:system-ui,-apple-system,sans-serif;color:var(--fg);
  background:var(--bg);max-width:48rem;margin:0 auto;padding:2.5rem 1.25rem;line-height:1.6}
  h1{font-size:2.2rem;margin:.2rem 0} h2{margin-top:2.2rem;border-bottom:1px solid #e7e7f0;padding-bottom:.3rem}
  .tag{color:var(--mut);font-size:1.15rem;margin:.3rem 0 1.5rem}
  code,pre{background:var(--code);border-radius:6px;font-family:ui-monospace,Menlo,monospace}
  code{padding:.1rem .35rem} pre{padding:.9rem 1rem;overflow:auto}
  a{color:var(--acc);text-decoration:none} a:hover{text-decoration:underline}
  .price{font-weight:700} ol,ul{padding-left:1.3rem} .muted{color:var(--mut);font-size:.92rem}
  .pill{display:inline-block;background:var(--code);color:var(--acc);border-radius:999px;padding:.15rem .7rem;font-size:.85rem;margin-right:.4rem}
</style>`

const landingHTML = `<!doctype html><html lang=en><head><meta charset=utf-8>
<meta name=viewport content="width=device-width,initial-scale=1">
<title>mago — autonomous agents that run your company</title>` + css + `</head><body>
<h1>mago</h1>
<p class=tag>Cheap autonomous AI agent teams that ship code over GitHub. <b>BYOK · CLI-only · €20/month.</b></p>
<p><span class=pill>no dashboard</span><span class=pill>BYOK — your key</span><span class=pill>Claude Code or tau</span><span class=pill>GitHub-native</span><span class=pill>agent-driven</span></p>
<p class=muted>Prefer a managed dashboard over the CLI? <a href="https://automaintainer.intrane.fr/">Automaintainer</a> is the premium, UI-driven alternative.</p>

<h2>What it is</h2>
<p>You file work as GitHub issues; an autonomous executive team — <b>CTO, CMO, Head of Product, Head of
Org Engineering</b> — picks them up, implements them as pull requests, reviews and merges. You're the
<b>CEO</b>. The agents run on <b>your</b> machine via your own agent harness — <b>Claude Code</b> (Sonnet,
on your Claude subscription) or <b><a href="https://github.com/javimosch/tau">tau</a></b> (with your
provider key) — so it's <b>BYOK</b> and mago never resells completions. There is no web panel: you (or
your own agent) drive everything from the <code>mago</code> CLI.</p>

<h2>How it works</h2>
<ol>
<li>Install the CLI (below) and pick your harness — <b>Claude Code</b> (Sonnet) or <a href="https://github.com/javimosch/tau">tau</a> + your provider key.</li>
<li><code>mago register</code> → <code>mago subscribe</code> (€20/month).</li>
<li>Install the mago GitHub App on your repos, then <code>mago link</code>.</li>
<li><code>mago serve --relay</code> — the worker dials out; GitHub events flow to your agents.</li>
<li>File issues (or label them <code>mago</code>); optionally <code>mago:clarify</code> for a plan-first pass, then <code>mago:go</code>. PRs ship.</li>
</ol>

<h2>Choosing a harness</h2>
<ul>
<li><b>Claude Code</b> — simplest if you already have a Claude subscription. Runs your agents on
<b>Sonnet</b>, no API key: <code>MAGO_PROVIDER=claude MAGO_MODEL=sonnet</code>.</li>
<li><b><a href="https://github.com/javimosch/tau">tau</a></b> — bring any provider key (opencode-go,
DeepSeek, OpenAI…). Think <b>pi</b>, but more lightweight and <b>agent-first</b> — built for agents to
drive, not humans.</li>
</ul>

<h2>Get started</h2>
<pre>curl -fsSL %s/install.sh | sh</pre>
<p>Then read the <a href="%s/operators">operator guide</a> — written for the agent that will drive mago.</p>

<h2>Pricing</h2>
<p class=price>€20 / month.</p>
<p class=muted>One flat plan. BYOK (your LLM key, your compute) — no per-token charges from us, no tiers.</p>
<p><b>48-hour free trial — no card.</b> <code>mago register</code> and your agents can ship a real PR before you ever pay.</p>

<p class=muted style="margin-top:3rem">mago · operated at <a href="%s">mago.intrane.fr</a> · onboarding is agent-driven, CLI-only.</p>
<p class=muted>🤖 AI agents: start at <a href="/llms.txt">/llms.txt</a> · humans: the <a href="/operators">operator guide</a>.</p>
<p class=muted>By <a href="https://automaintainer.intrane.fr/">Automaintainer</a> · Part of the <a href="https://intrane.fr">Intrane ecosystem</a>.</p>
</body></html>`

const installScript = `#!/bin/sh
# mago CLI installer — downloads the prebuilt binary from the platform.
set -e
BASE="%s"
DEST="${MAGO_BIN_DIR:-$HOME/.local/bin}"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in x86_64|amd64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; esac
case "$OS" in linux|darwin) ;; *) echo "unsupported OS: $OS (have linux, darwin)"; exit 1;; esac
mkdir -p "$DEST"
echo "Downloading mago ($OS/$ARCH) from $BASE/dl/mago ..."
curl -fsSL "$BASE/dl/mago?os=$OS&arch=$ARCH" -o "$DEST/mago"
chmod +x "$DEST/mago"
echo "Installed: $DEST/mago"
case ":$PATH:" in *":$DEST:"*) ;; *) echo "NOTE: add $DEST to your PATH";; esac
"$DEST/mago" version 2>/dev/null || true
echo ""
echo "Next: read the operator guide -> $BASE/operators"
echo "Then: mago register   (set your LLM key first; see the guide)"
`

const operatorsHTML = `<!doctype html><html lang=en><head><meta charset=utf-8>
<meta name=viewport content="width=device-width,initial-scale=1">
<title>mago — operator guide (for agents)</title>` + css + `</head><body>
<h1>Operator guide</h1>
<p class=tag>You are an AI agent operating mago on behalf of a human (the CEO). Drive everything via the
<code>mago</code> and <code>gh</code> CLIs. The human only: pays, installs the GitHub App, answers HITL/clarify questions, and says "go".</p>

<h2>0. Prerequisites</h2>
<ul>
<li><b>gh</b> on PATH + authenticated. Run <code>gh auth setup-git</code> once so the worker can push
branches to <b>private</b> repos (public repos work without it).</li>
<li><b>An agent harness (BYOK):</b> either
  <ul>
  <li><b>Claude Code</b> — have <code>claude</code> on PATH and logged in (your Claude subscription),
  then run with <code>MAGO_PROVIDER=claude MAGO_MODEL=sonnet</code>. No API key.</li>
  <li><b><a href="https://github.com/javimosch/tau">tau</a></b> (default) — on PATH, with your provider
  key in <code>~/.config/tau/config.json</code> → <code>{"keys": {"opencode-go": "sk-..."}}</code>
  (chmod 600) or <code>OPENCODE_API_KEY</code>.</li>
  </ul>
  Either way mago resells no completions — your harness, your key/subscription.</li>
</ul>

<h2>1. Install</h2>
<pre>curl -fsSL %s/install.sh | sh</pre>

<h2>2. Account + subscription</h2>
<pre>mago register --email you@co.com --password &lt;pw&gt;   # account + 48h NO-CARD trial (license cached)
mago account status                                 # -> plan: trial (active, ~Xh left), license_key
mago subscribe                                      # after the trial: Stripe link, the HUMAN pays (€20/mo)
mago billing                                        # manage/cancel the subscription (Stripe portal)</pre>
<p class=muted>The 48-hour trial runs the full loop immediately — no card. Subscribe anytime to continue past it.</p>

<h2>3. Connect GitHub</h2>
<p>The human installs the mago GitHub App on their repos (one browser click). Then:</p>
<pre>mago link --installation &lt;id&gt;    # entitles your repos (id is in the install URL)
mago link list                    # confirm entitled repos</pre>

<h2>4. Run the company</h2>
<pre>mago init ./company
mago project add &lt;name&gt; --repo owner/repo      # the repo your agents work on AND watch for issues
MAGO_TASK_LABEL=mago mago serve --relay -C ./company   # only act on issues labeled "mago"</pre>
<p class=muted><b>Single repo</b> (default): a lone project repo becomes your backlog automatically — its
issues are the work, PRs close them directly. <b>Multi-project</b> (one worker, several repos): set
<code>MAGO_GH_REPO</code> to the backlog/command repo, <code>mago project add</code> the others, then file
issues there labeled <code>project:&lt;name&gt;</code> (<code>mago task add "…" --project &lt;name&gt;</code>) to
open the PR on that project's repo. Note: proactive planning fills the backlog repo only — multi-project is for reactive dispatch.</p>

<h2>5. Operate via GitHub</h2>
<ul>
<li><b>File work</b> as issues. With <code>MAGO_TASK_LABEL=mago</code> the worker only takes issues labeled <code>mago</code> — so you can point it at a real repo safely; labeling an existing issue picks it up.</li>
<li><b>Clarify-first (optional):</b> label <code>mago:clarify</code> → the planner posts a plan + open questions; answer in comments (N rounds); add <code>mago:go</code> to implement.</li>
<li><b>Status labels:</b> <code>mago:in-progress</code>, <code>mago:blocked</code>, <code>mago:hitl</code> (needs the CEO), <code>agent:&lt;name&gt;</code>, <code>project:&lt;name&gt;</code>.</li>
<li>Implementers open PRs; the reviewer (Head of Org Engineering) merges. HITL questions appear as issue comments — the human answers and work resumes.</li>
</ul>

<p class=muted style="margin-top:3rem"><a href="%s">← mago.intrane.fr</a> · machine-readable version: <a href="/llms.txt">/llms.txt</a></p>
</body></html>`

// llmsText is the agent-facing operator skill (served at /llms.txt). Plain markdown so an
// operator agent can curl + follow it directly. %s = appURL ×3.
const llmsText = `# mago — operator guide for AI agents

> mago runs an autonomous executive team (CTO, CMO, Head of Product, Head of Org Engineering)
> that picks up GitHub issues and ships them as reviewed pull requests. You are the AI agent
> OPERATING mago on behalf of a human (the CEO). Drive everything via the ` + "`mago`" + ` and
> ` + "`gh`" + ` CLIs. BYOK (the human's LLM key stays on this machine) · CLI-only · €20/month.
> The human only: pays, installs the GitHub App, answers clarify/HITL questions, says "go".

## Prerequisites
- ` + "`gh`" + ` on PATH + authenticated (` + "`gh auth status`" + `). Run ` + "`gh auth setup-git`" + `
  once so the worker can push branches to private repos (public repos work without it).
- An agent harness (BYOK) — pick one:
  - **Claude Code:** ` + "`claude`" + ` on PATH + logged in (your Claude subscription); run with
    ` + "`MAGO_PROVIDER=claude MAGO_MODEL=sonnet`" + `. No API key. (Custom HOME? set ` + "`CLAUDE_CONFIG_DIR=~/.claude`" + `.)
  - **tau** (https://github.com/javimosch/tau, default): on PATH, key in ~/.config/tau/config.json ->
    {"keys":{"opencode-go":"sk-..."}} (chmod 600) or ` + "`OPENCODE_API_KEY`" + `.
  Either way mago resells no completions — your harness, your key/subscription.

## 1. Install
    curl -fsSL %s/install.sh | sh
  Installs the host-matched binary (linux/darwin x amd64/arm64) to ~/.local/bin/mago.

## 2. Account + subscription (the human pays)
    mago register --email you@co.com --password <pw>   # creates account + a 48h NO-CARD trial; license cached
    mago account status                                 # -> plan: trial (active, ~Xh left), license_key
    # During the trial you can run the full loop immediately. Then, to continue past 48h:
    mago subscribe                                      # prints the Stripe checkout link; HUMAN pays (€20/mo)
    mago billing                                        # prints the Stripe portal link (manage/cancel)

## 3. Connect GitHub
  The human installs the mago GitHub App on their repos (one browser click). Then:
    mago link --installation <id>    # entitle your repos (id is in the install URL)
    mago link list                   # confirm entitled repos

## 4. Run the company
    mago init ./company
    mago project add <name> --repo owner/repo   # the repo your agents work on AND watch for issues
    # Point at a real repo safely: only act on issues labeled "mago".
    MAGO_TASK_LABEL=mago mago serve --relay -C ./company   # worker dials out; agents wake on GitHub events
  Layouts:
  - Single repo (default): a lone "mago project add owner/repo" is auto-adopted as the backlog — its
    issues are the work, agents work that repo, PRs close its issues directly.
  - Multi-project (one worker, several repos): set MAGO_GH_REPO to the backlog/command repo, then
    "mago project add <name> --repo owner/repo" the others. File issues in the backlog repo labeled
    project:<name> (mago task add "<title>" --project <name>) and the PR opens on THAT project's repo.
    Caveat: proactive planning files into the backlog repo only -> multi-project is for REACTIVE dispatch.

## 5. Operate via GitHub
- File work as issues. With MAGO_TASK_LABEL=mago the worker only takes issues labeled "mago"
  (safe on a real repo; labeling an existing issue picks it up).
- Clarify-first (optional): label "mago:clarify" -> the planner posts a plan + open questions;
  answer in comments (N rounds); add "mago:go" to implement.
- Status labels: mago:in-progress, mago:blocked, mago:hitl (needs the CEO), agent:<name>, project:<name>.
- Implementers open PRs; the reviewer (Head of Org Engineering) merges. HITL questions appear as
  issue comments — the human answers and work resumes.

## Feedback (self-report friction)
    mago feedback "<what didn't click>" [--type bug|friction|feature|question]
  As the operating agent, run this whenever a mago command fails, is unclear, or blocks you — that
  friction is the most valuable signal for improving mago. It reaches the team as a triage item.

## Billing (human/CEO)
    mago subscribe   # start the €20/mo plan (Stripe checkout)
    mago billing     # open the Stripe customer portal: update card, invoices, cancel

## More
- **Authoritative, version-matched skills are embedded in your binary:** run ` + "`mago skills`" + `
  (operating · cli · fleet) — always current with the mago you installed, works offline. This page mirrors them.
- Human-readable guide: %s/operators
- Site: %s
`
