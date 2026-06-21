package main

import (
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
// directly, via a tunnel, or relayed by the platform) wakes it to reconcile in real
// time, instead of polling. An optional --heartbeat keeps a fallback cadence so missed
// events still get caught.
func cmdServe(args []string) error {
	dir, rest := parseCompanyDir(args)
	addr := ":8099"
	secret := os.Getenv("MAGO_WEBHOOK_SECRET")
	heartbeat := 0
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
		}
	}
	comp, err := loadCompany(dir)
	if err != nil {
		return err
	}

	w := &eventWorker{comp: comp, wake: make(chan string, 1)}
	go w.run()
	w.signal("startup")
	if heartbeat > 0 {
		go w.heartbeatLoop(time.Duration(heartbeat) * time.Second)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(rw http.ResponseWriter, _ *http.Request) { fmt.Fprintln(rw, "ok") })
	mux.HandleFunc("/webhook/github", w.handleWebhook(secret))
	fmt.Fprintf(os.Stderr, "mago serve: company %q (gh repo %q) listening on %s; webhook at /webhook/github\n",
		comp.Name, comp.ghRepo, addr)
	return http.ListenAndServe(addr, mux)
}

type eventWorker struct {
	comp *Company
	wake chan string // buffered(1): coalesces bursts so reconciles never overlap
}

func (w *eventWorker) signal(reason string) {
	select {
	case w.wake <- reason:
	default: // a reconcile is already pending/running; coalesce
	}
}

// run consumes wake signals and reconciles serially (one at a time).
func (w *eventWorker) run() {
	for reason := range w.wake {
		fmt.Fprintf(os.Stderr, "[wake] %s -> reconciling\n", reason)
		if _, err := reconcileOnce(w.comp); err != nil {
			fmt.Fprintf(os.Stderr, "[wake] reconcile error: %v\n", err)
		}
	}
}

func (w *eventWorker) heartbeatLoop(every time.Duration) {
	t := time.NewTicker(every)
	for range t.C {
		w.signal("heartbeat")
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
		reason, wake := classifyEvent(event, body)
		if wake {
			w.signal(reason)
		}
		fmt.Fprintf(rw, "ok event=%s wake=%v\n", event, wake)
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

// classifyEvent decides whether a GitHub webhook should wake the worker, and why.
func classifyEvent(event string, body []byte) (string, bool) {
	var p struct {
		Action  string `json:"action"`
		Comment struct {
			Body string `json:"body"`
		} `json:"comment"`
		Issue struct {
			Number int `json:"number"`
		} `json:"issue"`
		PullRequest struct {
			Number int `json:"number"`
		} `json:"pull_request"`
	}
	json.Unmarshal(body, &p)
	switch event {
	case "issues":
		switch p.Action {
		case "opened", "reopened", "assigned", "labeled":
			return fmt.Sprintf("issue #%d %s", p.Issue.Number, p.Action), true
		}
	case "issue_comment":
		// a human reply (HITL answer, new instruction) — mago's own comments are skipped
		if p.Action == "created" && !isMagoComment(p.Comment.Body) {
			return fmt.Sprintf("human comment on #%d", p.Issue.Number), true
		}
	case "pull_request":
		switch p.Action {
		case "opened", "reopened", "ready_for_review":
			return fmt.Sprintf("PR #%d %s", p.PullRequest.Number, p.Action), true
		}
	}
	return "", false
}
