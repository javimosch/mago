package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// relay.go is the GitHub-webhook relay (docs/SAAS.md phase 4). The platform owns one GitHub
// App / webhook ingress; NAT'd workers dial OUT and hold a streaming connection, and the
// platform pushes matching repo events down it. Transport is newline-delimited JSON over a
// long-lived HTTP response (stdlib-only — no WebSocket dependency), which is enough for the
// low volume of GitHub webhooks.

type relayMsg struct {
	Event string          `json:"event"`          // GitHub X-GitHub-Event (or "ping" keepalive)
	Body  json.RawMessage `json:"body,omitempty"` // raw webhook payload
}

type relayConn struct {
	license string
	repos   map[string]bool
	ch      chan relayMsg
}

type relayHub struct {
	mu      sync.Mutex
	workers map[string]*relayConn // license -> connection (one worker per account in v1)
}

func newRelayHub() *relayHub { return &relayHub{workers: map[string]*relayConn{}} }

func (h *relayHub) register(c *relayConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.workers[c.license]; old != nil {
		close(old.ch) // a new connection for the same license replaces the old one
	}
	h.workers[c.license] = c
}

func (h *relayHub) unregister(c *relayConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.workers[c.license] == c { // only if not already replaced
		delete(h.workers, c.license)
	}
}

// route delivers msg to every worker serving repo. Returns how many got it.
func (h *relayHub) route(repo string, msg relayMsg) int {
	h.mu.Lock()
	conns := make([]*relayConn, 0, len(h.workers))
	for _, c := range h.workers {
		if c.repos[repo] {
			conns = append(conns, c)
		}
	}
	h.mu.Unlock()
	n := 0
	for _, c := range conns {
		select {
		case c.ch <- msg:
			n++
		default: // worker is slow/backed up — drop; its heartbeat reconcile will catch up
		}
	}
	return n
}

// handleWorkerStream is the worker's dial-out endpoint: GET /ws/worker?token=<license>&repos=a/b,c/d
func (s *server) handleWorkerStream(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	u := s.store.GetByLicense(token)
	if u == nil {
		httpErr(w, 401, "unknown license")
		return
	}
	if u.Plan != "mago" { // license-gating: lapsed subscriptions are refused
		httpErr(w, 403, "subscription inactive")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpErr(w, 500, "streaming unsupported")
		return
	}
	repos := map[string]bool{}
	for _, rp := range strings.Split(r.URL.Query().Get("repos"), ",") {
		if rp = strings.TrimSpace(rp); rp != "" {
			repos[rp] = true
		}
	}
	conn := &relayConn{license: token, repos: repos, ch: make(chan relayMsg, 16)}
	s.hub.register(conn)
	defer s.hub.unregister(conn)
	log.Printf("relay: worker connected (user %d, repos=%v)", u.ID, keys(repos))

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	enc := json.NewEncoder(w)
	enc.Encode(relayMsg{Event: "ready"})
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done(): // worker disconnected
			log.Printf("relay: worker disconnected (user %d)", u.ID)
			return
		case msg, open := <-conn.ch:
			if !open { // replaced by a newer connection
				return
			}
			if enc.Encode(msg) != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if enc.Encode(relayMsg{Event: "ping"}) != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleGithubWebhook is the single GitHub ingress: POST /webhooks/github/<install>. It
// verifies the GitHub signature, then routes the event to the worker serving that repo.
func (s *server) handleGithubWebhook(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if !validGithubSig(s.ghWebhookSecret, r.Header.Get("X-Hub-Signature-256"), body) {
		httpErr(w, 401, "bad signature")
		return
	}
	var p struct {
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	json.Unmarshal(body, &p)
	if p.Repository.FullName == "" {
		w.WriteHeader(200) // nothing to route (e.g. ping event)
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	n := s.hub.route(p.Repository.FullName, relayMsg{Event: event, Body: body})
	log.Printf("relay: github %s on %s -> %d worker(s)", event, p.Repository.FullName, n)
	w.WriteHeader(200)
}

func validGithubSig(secret, sig string, body []byte) bool {
	if secret == "" || !strings.HasPrefix(sig, "sha256=") {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(want))
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
