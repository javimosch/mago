package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	if !u.entitled() { // license-gating: lapsed subscriptions / expired trials are refused
		msg := "subscription inactive — run `mago subscribe`"
		if u.Plan == "trial" {
			msg = "trial expired — run `mago subscribe` to continue"
		}
		httpErr(w, 403, msg)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpErr(w, 500, "streaming unsupported")
		return
	}
	claimed := map[string]bool{}
	for _, rp := range strings.Split(r.URL.Query().Get("repos"), ",") {
		if rp = strings.TrimSpace(rp); rp != "" {
			claimed[rp] = true
		}
	}
	// Entitlement: when the GitHub App is configured (multi-tenant), a worker may only receive
	// events for repos its account has claimed via an installation — never repos it merely
	// names. Without the App (local/single-tenant dev) we trust the claimed set.
	repos := claimed
	if s.enforceEntitlement {
		entitled := s.store.EntitledRepos(u.ID)
		repos = map[string]bool{}
		var dropped []string
		for r := range claimed {
			if entitled[r] {
				repos[r] = true
			} else {
				dropped = append(dropped, r)
			}
		}
		if len(dropped) > 0 {
			log.Printf("relay: user %d not entitled to %v — run `mago link`; ignoring", u.ID, dropped)
		}
	}
	conn := &relayConn{license: token, repos: repos, ch: make(chan relayMsg, 16)}
	s.hub.register(conn)
	defer s.hub.unregister(conn)
	log.Printf("relay: worker connected (user %d, repos=%v)", u.ID, keys(repos))
	s.store.LogEvent("worker_connect", u.ID, "repos="+strings.Join(keys(repos), ","))

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
			s.store.LogEvent("worker_disconnect", u.ID, "")
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
	event := r.Header.Get("X-GitHub-Event")

	// App lifecycle events maintain the installation registry; they are not relayed to workers.
	switch event {
	case "ping":
		w.WriteHeader(200)
		return
	case "installation", "installation_repositories":
		s.handleInstallationEvent(event, body)
		w.WriteHeader(200)
		return
	}

	var p struct {
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	json.Unmarshal(body, &p)
	if p.Repository.FullName == "" {
		w.WriteHeader(200) // nothing to route
		return
	}
	n := s.hub.route(p.Repository.FullName, relayMsg{Event: event, Body: body})
	log.Printf("relay: github %s on %s -> %d worker(s)", event, p.Repository.FullName, n)
	w.WriteHeader(200)
}

// handleInstallationEvent keeps the installations table in sync with GitHub. The payloads are
// signed (verified above), so the repo lists here are authoritative — a worker can't fake them.
func (s *server) handleInstallationEvent(event string, body []byte) {
	var p struct {
		Action       string `json:"action"`
		Installation struct {
			ID      int64 `json:"id"`
			Account struct {
				Login string `json:"login"`
			} `json:"account"`
		} `json:"installation"`
		Repositories []struct {
			FullName string `json:"full_name"`
		} `json:"repositories"`
		RepositoriesAdded []struct {
			FullName string `json:"full_name"`
		} `json:"repositories_added"`
		RepositoriesRemoved []struct {
			FullName string `json:"full_name"`
		} `json:"repositories_removed"`
	}
	json.Unmarshal(body, &p)
	id, login := p.Installation.ID, p.Installation.Account.Login
	names := func(rs []struct {
		FullName string `json:"full_name"`
	}) []string {
		out := make([]string, 0, len(rs))
		for _, r := range rs {
			if r.FullName != "" {
				out = append(out, r.FullName)
			}
		}
		return out
	}
	switch {
	case event == "installation" && p.Action == "deleted":
		s.store.DeleteInstallation(id)
		log.Printf("relay: installation %d (%s) deleted", id, login)
	case event == "installation": // created / new_permissions_accepted / suspend / unsuspend
		s.store.UpsertInstallation(id, login, names(p.Repositories))
		log.Printf("relay: installation %d (%s) %s -> %d repos", id, login, p.Action, len(p.Repositories))
	case event == "installation_repositories":
		s.store.MutateInstallationRepos(id, login, names(p.RepositoriesAdded), names(p.RepositoriesRemoved))
		log.Printf("relay: installation %d (%s) repos +%d -%d", id, login, len(p.RepositoriesAdded), len(p.RepositoriesRemoved))
	}
}

// handleInstallations is the authed account endpoint behind `mago link`:
//
//	GET  -> list the account's claimed installations and their repos
//	POST {installation_id} -> claim an installation for this account
func (s *server) handleInstallations(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.authUID(r)
	if !ok {
		httpErr(w, 401, "unauthorized")
		return
	}
	if r.Method == "POST" {
		var in struct {
			InstallationID int64 `json:"installation_id"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		if in.InstallationID <= 0 {
			httpErr(w, 400, "installation_id required")
			return
		}
		if err := s.store.ClaimInstallation(in.InstallationID, uid); err != nil {
			httpErr(w, 404, err.Error())
			return
		}
		s.store.LogEvent("linked", uid, fmt.Sprintf("installation %d (%d repos)", in.InstallationID, len(s.store.EntitledRepos(uid))))
	}
	writeJSON(w, 200, map[string]any{
		"installations": s.store.InstallationsForAccount(uid),
		"repos":         keys(s.store.EntitledRepos(uid)),
	})
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
