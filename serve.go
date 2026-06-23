package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// cmdServe runs the worker as an event-driven daemon: a GitHub webhook (delivered
// directly, via a tunnel, or relayed by the platform) wakes it in real time instead of
// polling. An optional --heartbeat keeps a fallback cadence so missed events still land.
func cmdServe(args []string) error {
	dir, rest, err := parseCompanyDir(args)
	if err != nil {
		return err
	}
	// Lifecycle subcommands: `mago serve stop|status [-C dir]`.
	if len(rest) > 0 {
		switch rest[0] {
		case "stop":
			return workerStop(dir)
		case "status":
			return workerStatus(dir)
		}
	}
	addr := ":8099"
	secret := os.Getenv("MAGO_WEBHOOK_SECRET")
	heartbeat := 0
	relay := false
	daemon := false    // --daemon: detach a supervisor (pidfile/log; restarts on crash)
	supervise := false // --supervise: internal mode run by the daemon's supervisor
	until := ""        // --until HH:MM: stop cleanly at this wall-clock time (native scheduled stop)
	startDelay := ""   // --start-delay <dur>: wait before starting (native fleet staggering)
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--daemon", "-d":
			daemon = true
		case "--supervise":
			supervise = true
		case "--addr":
			if i+1 < len(rest) {
				addr = rest[i+1]
				i++
			}
		case "--secret":
			if i+1 < len(rest) {
				secret = rest[i+1]
				i++
			}
		case "--heartbeat":
			if i+1 < len(rest) {
				heartbeat = atoiSafe(rest[i+1])
				i++
			}
		case "--until":
			if i+1 < len(rest) {
				until = rest[i+1]
				i++
			}
		case "--start-delay":
			if i+1 < len(rest) {
				startDelay = rest[i+1]
				i++
			}
		case "--relay":
			relay = true
		}
	}
	// --supervise (internal): the detached supervisor — keep a worker running, restart on crash.
	if supervise {
		return superviseWorker(stripArg(stripArg(args, "--supervise"), "--daemon"))
	}
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	// --daemon: detach a supervisor (pidfile + log) and return; the worker runs in the background.
	if daemon {
		return daemonizeWorker(comp, stripArg(args, "--daemon"))
	}
	warnIfNoProviderKey()
	if msg := comp.backlogRepoWarning(relay); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}

	// --start-delay: stagger fleet workers without an OS `sleep` wrapper.
	if startDelay != "" {
		d, err := time.ParseDuration(startDelay)
		if err != nil {
			return fmt.Errorf("--start-delay must be a duration (e.g. 15m, 900s): %w", err)
		}
		fmt.Fprintf(os.Stderr, "[lifecycle] start-delay %s before serving\n", d)
		time.Sleep(d)
	}
	// --until HH:MM: native scheduled stop (replaces a cron kill). Exits cleanly at the next HH:MM.
	if until != "" {
		d, err := untilDuration(until)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "[lifecycle] will stop at %s (in %s)\n", until, d.Round(time.Minute))
		go func() {
			time.Sleep(d)
			fmt.Fprintf(os.Stderr, "[lifecycle] reached --until %s — stopping\n", until)
			os.Exit(0)
		}()
	}

	w := &eventWorker{comp: comp, wake: make(chan wakeEvent, 64)}
	go w.run()
	w.signal(wakeEvent{reason: "startup"})
	if heartbeat > 0 {
		go w.heartbeatLoop(time.Duration(heartbeat) * time.Second)
	}
	// Proactive planning runs on the live mode's cadence (0 = reactive). Always started; it self-gates
	// so the mode can be switched at runtime (mago mode / mago worker mode) without a restart.
	go w.proactiveLoop()
	repos := comp.repos()
	reposStr := "none — add with `mago project add <name> --repo owner/repo`"
	if len(repos) > 0 {
		reposStr = strings.Join(repos, ", ")
	}

	// --relay: dial out to the platform for GitHub events. The relay connection is outbound and
	// long-lived, so there's NO inbound webhook — we don't bind a local port (avoids a needless
	// listener and lets several relay workers share a box). runRelay blocks, reconnecting until killed.
	if relay {
		fmt.Fprintf(os.Stderr, "mago serve: company %q (repos: %s); relay -> platform (no inbound port)\n",
			comp.Name, reposStr)
		fmt.Fprintln(os.Stderr, "operate: file work as issues labeled `mago` · check in: `mago digest` · "+
			"tune live: `mago mode <reactive|proactive|verified|...>` · report friction: `mago feedback \"...\"`")
		runRelay(context.Background(), w, loadConfig(), repos)
		return nil
	}

	// Tunnel/direct mode: bind the local webhook listener.
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(rw http.ResponseWriter, _ *http.Request) { fmt.Fprintln(rw, "ok") })
	mux.HandleFunc("/webhook/github", w.handleWebhook(secret))
	fmt.Fprintf(os.Stderr, "mago serve: company %q (repos: %s) listening on %s; webhook at /webhook/github\n",
		comp.Name, reposStr, addr)
	return http.ListenAndServe(addr, mux)
}

// wakeEvent carries why we woke and how to act: a single agent to wake (target), a PR to
// review (prRepo/prNum), or — when all are empty — a full reconcile (routing + all agents).
type wakeEvent struct {
	reason    string
	target    string
	prRepo    string
	prNum     int
	comms     bool   // a merged PR -> CMO drafts a release note (non-code loop)
	prTitle   string // merged PR title, for the comms note
	proactive bool   // cadence tick -> planner proposes new backlog from the mission
}

type eventWorker struct {
	comp *Company
	wake chan wakeEvent
}

func (w *eventWorker) signal(ev wakeEvent) {
	// Recurring wakes (heartbeat reconcile, proactive cadence) are idempotent — another fires soon.
	// While a long tick blocks the loop they can flood the queue and starve one-shot events (PR
	// review, comms, a targeted tick), so drop them once the queue is half full; one-shot events
	// keep trying (and the larger buffer absorbs the burst).
	coalescable := ev.proactive || ev.reason == "heartbeat"
	if coalescable && len(w.wake) > cap(w.wake)/2 {
		return
	}
	select {
	case w.wake <- ev:
	default: // queue genuinely full — a future event/heartbeat will catch up
	}
}

// run consumes wake events serially: a targeted event runs just that agent; otherwise
// a full reconcile.
func (w *eventWorker) run() {
	for ev := range w.wake {
		switch {
		case ev.proactive:
			if w.comp.guardBudget("proactive planning") {
				continue
			}
			fmt.Fprintf(os.Stderr, "[wake] %s -> planner proposing backlog\n", ev.reason)
			if w.comp.proposeBacklog() > 0 { // only count cycles that actually filed work
				w.comp.recordAction()
			}
		case ev.comms:
			if !w.comp.modeComms() { // non-code flow toggled off (live)
				continue
			}
			if w.comp.guardBudget("release note") {
				continue
			}
			fmt.Fprintf(os.Stderr, "[wake] %s -> CMO drafting release note for PR #%d in %s\n", ev.reason, ev.prNum, ev.prRepo)
			if w.comp.shipReleaseNote(ev.prRepo, ev.prNum, ev.prTitle) {
				w.comp.recordAction()
			}
		case ev.prRepo != "":
			if w.comp.guardBudget("PR review") {
				continue
			}
			fmt.Fprintf(os.Stderr, "[wake] %s -> reviewing PR #%d in %s\n", ev.reason, ev.prNum, ev.prRepo)
			if w.comp.reviewPR(ev.prRepo, ev.prNum) {
				w.comp.recordAction()
			}
		case ev.target != "":
			if w.comp.guardBudget("tick") {
				continue
			}
			fmt.Fprintf(os.Stderr, "[wake] %s -> waking %s\n", ev.reason, ev.target)
			if _, err := runTick(w.comp, ev.target); err != nil {
				fmt.Fprintf(os.Stderr, "[wake] tick %s error: %v\n", ev.target, err)
			}
			w.comp.recordAction()
		default:
			if w.comp.guardBudget("reconcile") {
				continue
			}
			fmt.Fprintf(os.Stderr, "[wake] %s -> reconciling\n", ev.reason)
			worked, err := reconcileOnce(w.comp)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[wake] reconcile error: %v\n", err)
			}
			if worked { // only count cycles that actually did work (idle reconciles are free)
				w.comp.recordAction()
			}
		}
	}
}

func (w *eventWorker) heartbeatLoop(every time.Duration) {
	t := time.NewTicker(every)
	for range t.C {
		w.signal(wakeEvent{reason: "heartbeat"})
	}
}

// untilDuration returns the time from now until the next occurrence of HH:MM (today if still
// ahead, otherwise tomorrow). Powers `mago serve --until` (native scheduled stop).
func untilDuration(hhmm string) (time.Duration, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(hhmm))
	if err != nil {
		return 0, fmt.Errorf("--until must be HH:MM 24h (e.g. 09:00): %w", err)
	}
	now := time.Now()
	target := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	if !target.After(now) {
		target = target.Add(24 * time.Hour)
	}
	return time.Until(target), nil
}

// proactiveLoop ticks the planner to propose backlog on the live mode cadence. It re-reads the mode
// each cycle, so enabling/disabling/retuning proactive (mago mode / mago worker mode) takes effect
// without a restart. When reactive (cadence 0) it idles, polling the mode every 30s.
func (w *eventWorker) proactiveLoop() {
	for {
		secs := w.comp.modeProactive()
		if secs <= 0 {
			time.Sleep(30 * time.Second)
			continue
		}
		time.Sleep(time.Duration(secs) * time.Second)
		if w.comp.modeProactive() > 0 { // still proactive after the sleep?
			w.signal(wakeEvent{reason: "proactive cadence", proactive: true})
		}
	}
}

func (w *eventWorker) handleWebhook(secret string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if secret != "" && !validSignature(secret, r.Header.Get("X-Hub-Signature-256"), body) {
			http.Error(rw, "bad signature", http.StatusUnauthorized)
			return
		}
		event := r.Header.Get("X-GitHub-Event")
		ev, wake := classifyEvent(event, body)
		if wake {
			w.signal(ev)
		}
		fmt.Fprintf(rw, "ok event=%s wake=%v target=%q\n", event, wake, ev.target)
	}
}

func validSignature(secret, sig string, body []byte) bool {
	if !strings.HasPrefix(sig, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(want))
}

// classifyEvent decides whether a GitHub webhook should wake the worker, and whether it
// can target a single agent (the issue's agent:<name> owner) instead of a full reconcile.
func classifyEvent(event string, body []byte) (wakeEvent, bool) {
	var p struct {
		Action  string `json:"action"`
		Comment struct {
			Body string `json:"body"`
		} `json:"comment"`
		Issue struct {
			Number int `json:"number"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"issue"`
		PullRequest struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Merged bool   `json:"merged"`
			Head   struct {
				Ref string `json:"ref"`
			} `json:"head"`
		} `json:"pull_request"`
		Label struct {
			Name string `json:"name"`
		} `json:"label"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	json.Unmarshal(body, &p)

	owner := func() string {
		for _, l := range p.Issue.Labels {
			if strings.HasPrefix(l.Name, "agent:") {
				return strings.TrimPrefix(l.Name, "agent:")
			}
		}
		return ""
	}

	switch event {
	case "issues":
		switch p.Action {
		case "opened", "reopened", "assigned":
			return wakeEvent{reason: fmt.Sprintf("issue #%d %s", p.Issue.Number, p.Action)}, true
		case "labeled":
			// Wake only on HUMAN-applied control labels: the scoped-backlog label (MAGO_TASK_LABEL),
			// or the clarify/go labels. mago never applies these itself (it toggles agent:/mago:in-
			// progress/hitl), so this can't self-wake in a loop.
			tl := os.Getenv("MAGO_TASK_LABEL")
			if p.Label.Name == "mago:clarify" || p.Label.Name == "mago:go" || (tl != "" && p.Label.Name == tl) {
				return wakeEvent{reason: fmt.Sprintf("issue #%d labeled %s", p.Issue.Number, p.Label.Name)}, true
			}
		}
	case "issue_comment":
		// a human reply (HITL answer, new instruction); mago's own comments are skipped.
		if p.Action == "created" && !isMagoComment(p.Comment.Body) {
			return wakeEvent{reason: fmt.Sprintf("human comment on #%d", p.Issue.Number), target: owner()}, true
		}
	case "pull_request":
		switch p.Action {
		case "opened", "reopened", "ready_for_review":
			return wakeEvent{
				reason: fmt.Sprintf("PR #%d %s", p.PullRequest.Number, p.Action),
				prRepo: p.Repository.FullName,
				prNum:  p.PullRequest.Number,
			}, true
		case "closed":
			// Beyond-code loop: a merged implementer PR (mago/task-*) wakes the CMO to draft a release
			// note. The comms toggle is checked live in the run loop (modeComms), so the wake is always
			// emitted here; only mago/task-* branches qualify (the comms' own work isn't on those).
			if p.PullRequest.Merged && strings.HasPrefix(p.PullRequest.Head.Ref, "mago/task-") {
				return wakeEvent{
					reason:  fmt.Sprintf("PR #%d merged", p.PullRequest.Number),
					prRepo:  p.Repository.FullName,
					prNum:   p.PullRequest.Number,
					prTitle: p.PullRequest.Title,
					comms:   true,
				}, true
			}
		}
	}
	return wakeEvent{}, false
}
