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
	dir, rest := parseCompanyDir(args)
	addr := ":8099"
	secret := os.Getenv("MAGO_WEBHOOK_SECRET")
	heartbeat := 0
	relay := false
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
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
		case "--relay":
			relay = true
		}
	}
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}
	warnIfNoProviderKey()

	w := &eventWorker{comp: comp, wake: make(chan wakeEvent, 8)}
	go w.run()
	w.signal(wakeEvent{reason: "startup"})
	if heartbeat > 0 {
		go w.heartbeatLoop(time.Duration(heartbeat) * time.Second)
	}
	// --relay: dial out to the platform for GitHub events instead of needing a public tunnel.
	if relay {
		go runRelay(context.Background(), w, loadConfig(), comp.repos())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(rw http.ResponseWriter, _ *http.Request) { fmt.Fprintln(rw, "ok") })
	mux.HandleFunc("/webhook/github", w.handleWebhook(secret))
	repos := comp.repos()
	reposStr := "none — add with `mago project add <name> --repo owner/repo`"
	if len(repos) > 0 {
		reposStr = strings.Join(repos, ", ")
	}
	fmt.Fprintf(os.Stderr, "mago serve: company %q (repos: %s) listening on %s; webhook at /webhook/github%s\n",
		comp.Name, reposStr, addr, ifStr(relay, "; relay -> platform", ""))
	return http.ListenAndServe(addr, mux)
}

// wakeEvent carries why we woke and how to act: a single agent to wake (target), a PR to
// review (prRepo/prNum), or — when all are empty — a full reconcile (routing + all agents).
type wakeEvent struct {
	reason string
	target string
	prRepo string
	prNum  int
}

type eventWorker struct {
	comp *Company
	wake chan wakeEvent
}

func (w *eventWorker) signal(ev wakeEvent) {
	select {
	case w.wake <- ev:
	default: // queue full — heartbeat / next event will catch up
	}
}

// run consumes wake events serially: a targeted event runs just that agent; otherwise
// a full reconcile.
func (w *eventWorker) run() {
	for ev := range w.wake {
		switch {
		case ev.prRepo != "":
			fmt.Fprintf(os.Stderr, "[wake] %s -> reviewing PR #%d in %s\n", ev.reason, ev.prNum, ev.prRepo)
			w.comp.reviewPR(ev.prRepo, ev.prNum)
		case ev.target != "":
			fmt.Fprintf(os.Stderr, "[wake] %s -> waking %s\n", ev.reason, ev.target)
			if _, err := runTick(w.comp, ev.target); err != nil {
				fmt.Fprintf(os.Stderr, "[wake] tick %s error: %v\n", ev.target, err)
			}
		default:
			fmt.Fprintf(os.Stderr, "[wake] %s -> reconciling\n", ev.reason)
			if _, err := reconcileOnce(w.comp); err != nil {
				fmt.Fprintf(os.Stderr, "[wake] reconcile error: %v\n", err)
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
			Number int `json:"number"`
		} `json:"pull_request"`
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
		// not "labeled": mago changes its own labels constantly (agent:, mago:in-progress)
		// and would wake itself in a loop.
		switch p.Action {
		case "opened", "reopened", "assigned":
			return wakeEvent{reason: fmt.Sprintf("issue #%d %s", p.Issue.Number, p.Action)}, true
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
		}
	}
	return wakeEvent{}, false
}
