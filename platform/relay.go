package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
	Event   string          `json:"event"`             // GitHub X-GitHub-Event (or "ping" keepalive)
	Body    json.RawMessage `json:"body,omitempty"`    // raw webhook payload
	Version string          `json:"version,omitempty"` // latest CLI version for the worker's os/arch (self-update)
}

type relayConn struct {
	license     string
	worker      string // worker id (hostname or MAGO_WORKER_ID) — an account may run several
	key         string // license + "\x00" + worker: identifies one worker's connection
	plat        string // worker os-arch (e.g. linux-amd64) — picks the binary version to advertise
	repos       map[string]bool
	ch          chan relayMsg
	connectedAt time.Time // to detect collision churn (two workers sharing an id evicting each other)
}

type relayHub struct {
	mu      sync.Mutex
	workers map[string]*relayConn // key (license+worker) -> connection; many workers per account
}

func newRelayHub() *relayHub { return &relayHub{workers: map[string]*relayConn{}} }

// register adds a connection, displacing any prior one for the same key (the SAME worker reconnecting
// replaces its old connection). Returns the displaced conn (or nil) so the caller can detect the
// pathological case: two DIFFERENT workers sharing one MAGO_WORKER_ID, which evict each other forever.
func (h *relayHub) register(c *relayConn) *relayConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	old := h.workers[c.key]
	if old != nil {
		close(old.ch)
	}
	h.workers[c.key] = c
	return old
}

func (h *relayHub) unregister(c *relayConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.workers[c.key] == c { // only if not already replaced
		delete(h.workers, c.key)
	}
}

// route delivers msg to exactly ONE worker serving repo, chosen stably by repo so that two workers
// serving the same repo never both process (and collide on) the same event. Returns 1 if delivered.
func (h *relayHub) route(repo string, msg relayMsg) int {
	h.mu.Lock()
	var conns []*relayConn
	for _, c := range h.workers {
		if c.repos[repo] {
			conns = append(conns, c)
		}
	}
	h.mu.Unlock()
	if len(conns) == 0 {
		return 0
	}
	sort.Slice(conns, func(i, j int) bool { return conns[i].key < conns[j].key })
	pick := conns[repoHash(repo)%uint32(len(conns))]
	select {
	case pick.ch <- msg:
		return 1
	default: // picked worker is backed up — drop; its heartbeat reconcile will catch up
		return 0
	}
}

func repoHash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// controlMode pushes a control frame to an account's worker(s) — a specific one by worker id, or
// all of them — and returns the worker ids that received it. Powers `mago worker mode`.
func (h *relayHub) controlMode(license, worker string, all bool, msg relayMsg) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var hit []string
	for _, c := range h.workers {
		if c.license != license {
			continue
		}
		if all || c.worker == worker {
			select {
			case c.ch <- msg:
				hit = append(hit, c.worker)
			default:
			}
		}
	}
	return hit
}

// handleWorkerControl lets the operator switch a connected worker's mode live over the relay
// (POST /api/worker/control: {worker|all, tokens}). JWT-authed; routes to the account's own workers.
func (s *server) handleWorkerControl(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.authUID(r)
	if !ok {
		httpErr(w, 401, "unauthorized")
		return
	}
	u := s.store.GetByID(uid)
	if u == nil || u.LicenseKey == "" {
		httpErr(w, 404, "no license")
		return
	}
	var in struct {
		Worker string   `json:"worker"`
		All    bool     `json:"all"`
		Tokens []string `json:"tokens"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Tokens) == 0 {
		httpErr(w, 400, "tokens required")
		return
	}
	bodyB, _ := json.Marshal(map[string][]string{"tokens": in.Tokens})
	hit := s.hub.controlMode(u.LicenseKey, in.Worker, in.All, relayMsg{Event: "control", Body: bodyB})
	s.store.LogEvent("mode", uid, strings.Join(in.Tokens, " ")+" → "+strings.Join(hit, ","))
	writeJSON(w, 200, map[string]any{"updated": len(hit), "workers": hit})
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
	workerName := strings.TrimSpace(r.URL.Query().Get("worker"))
	if workerName == "" {
		workerName = "default"
	}
	plat := platOf(r.URL.Query().Get("os"), r.URL.Query().Get("arch"))
	conn := &relayConn{license: token, worker: workerName, key: token + "\x00" + workerName, plat: plat, repos: repos, ch: make(chan relayMsg, 16), connectedAt: time.Now()}
	if old := s.hub.register(conn); old != nil && time.Since(old.connectedAt) < 45*time.Second {
		// The just-displaced connection was very short-lived -> two workers are sharing this
		// MAGO_WORKER_ID and evicting each other on a loop. Surface it loudly instead of churning silently.
		log.Printf("relay: WARNING worker-id %q (user %d) re-registered after only %s — likely TWO workers sharing MAGO_WORKER_ID; they will churn. Give each a distinct id.", workerName, u.ID, time.Since(old.connectedAt).Round(time.Second))
		s.store.LogEvent("worker_collision", u.ID, workerName+" — duplicate MAGO_WORKER_ID (two workers sharing one id churn on the relay; set a distinct MAGO_WORKER_ID per worker)")
	}
	defer s.hub.unregister(conn)
	log.Printf("relay: worker %q connected (user %d, repos=%v)", workerName, u.ID, keys(repos))
	s.store.LogEvent("worker_connect", u.ID, workerName+" · repos="+strings.Join(keys(repos), ","))

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	enc := json.NewEncoder(w)
	// The ready/ping frames carry the latest CLI version for this worker's os/arch so it can
	// self-update (worker side: maybeSelfUpdate). cliVersion caches per file mtime, so this is cheap.
	enc.Encode(relayMsg{Event: "ready", Version: cliVersion(plat)})
	flusher.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done(): // worker disconnected
			log.Printf("relay: worker %q disconnected (user %d)", workerName, u.ID)
			s.store.LogEvent("worker_disconnect", u.ID, workerName)
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
			if enc.Encode(relayMsg{Event: "ping", Version: cliVersion(plat)}) != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// platOf normalizes a worker's reported os/arch to the "os-arch" binary key, defaulting to
// linux-amd64 (what older workers that don't report get).
func platOf(osName, arch string) string {
	if osName == "" {
		osName = "linux"
	}
	if arch == "" {
		arch = "amd64"
	}
	return osName + "-" + arch
}

// cliVersion returns sha256[:12] of the published CLI binary for plat (mago-<plat> in MAGO_CLI_DIR),
// or "" if it can't be read. Cached per file mtime so it isn't re-hashed on every ping frame.
func cliVersion(plat string) string {
	dir := strings.TrimSpace(os.Getenv("MAGO_CLI_DIR"))
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "mago-"+plat)
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	mt := fi.ModTime().UnixNano()
	cliVerMu.Lock()
	defer cliVerMu.Unlock()
	if c, ok := cliVerCache[plat]; ok && c.mtime == mt {
		return c.ver
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	ver := hex.EncodeToString(h.Sum(nil))[:12]
	cliVerCache[plat] = cliVerEntry{mtime: mt, ver: ver}
	return ver
}

type cliVerEntry struct {
	mtime int64
	ver   string
}

var (
	cliVerMu    sync.Mutex
	cliVerCache = map[string]cliVerEntry{}
)

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
		Action     string `json:"action"`
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
	// Aggregate relay usage: a relayed GitHub event = the operator's repo activity that mago is acting
	// on, which is real adoption depth (issues filed, PRs flowing, comments) — the demand signal.
	if acct := s.store.AccountForRepo(p.Repository.FullName); acct != 0 {
		s.store.RecordGHEvent(acct, p.Repository.FullName, event, p.Action)
	}
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
		// Best-effort full resync from GitHub: an "All repositories" install never gets a repo
		// list via the installation webhook (only "select repositories" does), so repos_json can
		// be permanently stale for repos added/existing before any installation_repositories
		// delta touched them. Claiming is a natural, low-frequency moment to true it up. Never
		// fail the claim itself if this errors (rate limit, transient API issue, etc.).
		if repos, err := listInstallationRepos(in.InstallationID); err == nil {
			s.store.UpsertInstallation(in.InstallationID, "", repos)
		} else {
			log.Printf("resync installation %d repos: %v", in.InstallationID, err)
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
