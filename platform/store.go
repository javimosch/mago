package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no cgo), registered as "sqlite"
)

type User struct {
	ID             int64
	Email          string
	PasswordHash   string
	Plan           string // "free" | "trial" | "mago"
	StripeCustomer string
	StripeSub      string
	LicenseKey     string
	TrialEnds      int64 // unix; >now while a no-card trial is live
	CreatedAt      int64
}

// trialDuration is the no-card trial window granted at signup.
const trialDuration = 48 * time.Hour

// trialActive reports whether the user is within an unexpired no-card trial.
func (u *User) trialActive() bool { return u.Plan == "trial" && u.TrialEnds > time.Now().Unix() }

// entitled reports whether the worker may connect: a paid plan, or a live trial.
func (u *User) entitled() bool {
	return u.Plan == "mago" || u.Plan == "founding" || u.trialActive()
}

// Store is the platform's SQLite-backed persistence (schema in docs/SAAS.md). The method set
// matches what the handlers expect; Update applies a mutation to one user row transactionally.
type Store struct {
	db *sql.DB
}

func openStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite single-writer; keep it simple and serialized
	schema := `
CREATE TABLE IF NOT EXISTS users (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  email           TEXT UNIQUE NOT NULL,
  password_hash   TEXT NOT NULL,
  plan            TEXT NOT NULL DEFAULT 'free',
  stripe_customer TEXT NOT NULL DEFAULT '',
  stripe_sub      TEXT NOT NULL DEFAULT '',
  license_key     TEXT NOT NULL DEFAULT '',
  trial_ends      INTEGER NOT NULL DEFAULT 0,
  created_at      INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS stripe_events (
  id      TEXT PRIMARY KEY,
  seen_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  ts      INTEGER NOT NULL,
  kind    TEXT NOT NULL,              -- signup | subscribed | canceled | worker_connect | worker_disconnect | linked
  user_id INTEGER NOT NULL DEFAULT 0,
  detail  TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS installations (
  installation_id INTEGER PRIMARY KEY,        -- GitHub App installation id
  account_id      INTEGER NOT NULL DEFAULT 0, -- mago user id; 0 = unclaimed (set by ` + "`mago link`" + `)
  github_login    TEXT NOT NULL DEFAULT '',   -- the org/user the app is installed on
  repos_json      TEXT NOT NULL DEFAULT '[]', -- repos this installation covers (from GitHub webhooks)
  updated_at      INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS repo_grants (
  account_id INTEGER NOT NULL,                -- mago user id entitled to the repo
  repo       TEXT NOT NULL,                   -- owner/repo
  PRIMARY KEY (account_id, repo)
);
CREATE TABLE IF NOT EXISTS gh_events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  ts         INTEGER NOT NULL,
  account_id INTEGER NOT NULL DEFAULT 0,      -- owning mago account (resolved from the repo)
  repo       TEXT NOT NULL,                   -- owner/repo
  event      TEXT NOT NULL,                   -- X-GitHub-Event: issues | pull_request | issue_comment | ...
  action     TEXT NOT NULL DEFAULT ''         -- payload action: opened | closed | labeled | ...
);
CREATE INDEX IF NOT EXISTS idx_gh_events_acct_ts ON gh_events (account_id, ts);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	// Migrate pre-trial DBs: add trial_ends if the users table predates it (ignore "duplicate column").
	db.Exec("ALTER TABLE users ADD COLUMN trial_ends INTEGER NOT NULL DEFAULT 0")
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// --- relay usage aggregation: the operator's repo activity that mago acts on = real adoption depth ---

// AccountUsage is a per-account rollup of relayed GitHub activity over a window.
type AccountUsage struct {
	AccountID int64    `json:"account_id"`
	Email     string   `json:"email"`
	Plan      string   `json:"plan"`
	Repos     []string `json:"repos"`    // distinct repos active in the window
	Total     int      `json:"total"`    // all relayed events
	Issues    int      `json:"issues"`   // `issues` events (work filed / labeled)
	PRs       int      `json:"prs"`      // `pull_request` events (deliverables flowing)
	Comments  int      `json:"comments"` // `issue_comment` events (operator interaction / HITL)
	Other     int      `json:"other"`    // any other relayed event
	LastTs    int64    `json:"last_ts"`  // most recent event (0 = none)
}

// AccountForRepo returns the mago account entitled to a repo (0 if none) — used to attribute a
// relayed GitHub event to the operator whose worker is acting on it. Mirrors EntitledRepos' two
// sources: direct repo_grants, then claimed GitHub App installations (whose repos_json lists the repo
// — the real-user path, since App-entitled repos never land in repo_grants).
func (s *Store) AccountForRepo(repo string) int64 {
	var id int64
	if s.db.QueryRow("SELECT account_id FROM repo_grants WHERE repo = ? LIMIT 1", repo).Scan(&id) == nil && id != 0 {
		return id
	}
	rows, err := s.db.Query("SELECT account_id, repos_json FROM installations WHERE account_id != 0")
	if err != nil {
		return 0
	}
	defer rows.Close()
	for rows.Next() {
		var acct int64
		var raw string
		if rows.Scan(&acct, &raw) != nil {
			continue
		}
		var repos []string
		if json.Unmarshal([]byte(raw), &repos) != nil {
			continue
		}
		for _, r := range repos {
			if r == repo {
				return acct
			}
		}
	}
	return 0
}

// RecordGHEvent logs one relayed GitHub event for usage aggregation (best-effort; never blocks).
func (s *Store) RecordGHEvent(accountID int64, repo, event, action string) {
	s.db.Exec("INSERT INTO gh_events (ts, account_id, repo, event, action) VALUES (?, ?, ?, ?, ?)",
		time.Now().Unix(), accountID, repo, event, action)
}

// UsageForAccount rolls up an account's relayed activity over the last `days`.
func (s *Store) UsageForAccount(accountID int64, days int) AccountUsage {
	if days <= 0 {
		days = 7
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	u := AccountUsage{AccountID: accountID}
	if usr := s.GetByID(accountID); usr != nil {
		u.Email, u.Plan = usr.Email, usr.Plan
	}
	if rows, err := s.db.Query("SELECT event, COUNT(*), MAX(ts) FROM gh_events WHERE account_id=? AND ts>=? GROUP BY event", accountID, since); err == nil {
		defer rows.Close()
		for rows.Next() {
			var ev string
			var c int
			var mx int64
			if rows.Scan(&ev, &c, &mx) != nil {
				continue
			}
			u.Total += c
			if mx > u.LastTs {
				u.LastTs = mx
			}
			switch ev {
			case "issues":
				u.Issues += c
			case "pull_request":
				u.PRs += c
			case "issue_comment":
				u.Comments += c
			default:
				u.Other += c
			}
		}
	}
	if rows, err := s.db.Query("SELECT DISTINCT repo FROM gh_events WHERE account_id=? AND ts>=? ORDER BY repo", accountID, since); err == nil {
		defer rows.Close()
		for rows.Next() {
			var r string
			if rows.Scan(&r) == nil {
				u.Repos = append(u.Repos, r)
			}
		}
	}
	return u
}

// UsageByAccount returns each account with relayed activity over the window, most-active first —
// the operator's "who is actually using mago" view.
func (s *Store) UsageByAccount(days int) []AccountUsage {
	if days <= 0 {
		days = 7
	}
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	rows, err := s.db.Query("SELECT account_id FROM gh_events WHERE ts>=? GROUP BY account_id ORDER BY COUNT(*) DESC", since)
	if err != nil {
		return nil
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	out := make([]AccountUsage, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.UsageForAccount(id, days))
	}
	return out
}

const userCols = "id, email, password_hash, plan, stripe_customer, stripe_sub, license_key, trial_ends, created_at"

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Plan, &u.StripeCustomer, &u.StripeSub, &u.LicenseKey, &u.TrialEnds, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (s *Store) GetByEmail(email string) *User {
	u, _ := scanUser(s.db.QueryRow("SELECT "+userCols+" FROM users WHERE email = ?", email))
	return u
}

func (s *Store) GetByID(id int64) *User {
	u, _ := scanUser(s.db.QueryRow("SELECT "+userCols+" FROM users WHERE id = ?", id))
	return u
}

func (s *Store) GetByLicense(k string) *User {
	if k == "" {
		return nil
	}
	u, _ := scanUser(s.db.QueryRow("SELECT "+userCols+" FROM users WHERE license_key = ?", k))
	return u
}

func (s *Store) Create(email, hash string) (*User, error) {
	res, err := s.db.Exec(
		"INSERT INTO users (email, password_hash, plan, created_at) VALUES (?, ?, 'free', ?)",
		email, hash, time.Now().Unix())
	if err != nil {
		return nil, fmt.Errorf("email already registered") // UNIQUE(email) violation
	}
	id, _ := res.LastInsertId()
	return s.GetByID(id), nil
}

// Update loads the user, applies fn to it, and writes the mutated row back transactionally.
func (s *Store) Update(id int64, fn func(*User)) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	u, err := scanUser(tx.QueryRow("SELECT "+userCols+" FROM users WHERE id = ?", id))
	if err != nil || u == nil {
		return err
	}
	fn(u)
	if _, err := tx.Exec(
		"UPDATE users SET plan=?, stripe_customer=?, stripe_sub=?, license_key=?, trial_ends=? WHERE id=?",
		u.Plan, u.StripeCustomer, u.StripeSub, u.LicenseKey, u.TrialEnds, u.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// --- onboarding observability ---

// Event is a recorded onboarding moment (signup, subscribe, worker connect, …) for `mago-platform activity`.
type Event struct {
	TS     int64
	Kind   string
	UserID int64
	Email  string
	Detail string
}

// LogEvent records an onboarding event (best-effort; never blocks the request path).
func (s *Store) LogEvent(kind string, uid int64, detail string) {
	s.db.Exec("INSERT INTO events (ts, kind, user_id, detail) VALUES (?, ?, ?, ?)", time.Now().Unix(), kind, uid, detail)
	// Send Telegram notification for high-signal events if configured.
	var email string
	s.db.QueryRow("SELECT COALESCE(email, '') FROM users WHERE id = ?", uid).Scan(&email)
	notifyOnEvent(kind, uid, email, detail)
}

// RecentEvents returns the newest events first, resolving the account email when known.
func (s *Store) RecentEvents(limit int) []Event {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		"SELECT e.ts, e.kind, e.user_id, COALESCE(u.email,''), e.detail FROM events e "+
			"LEFT JOIN users u ON u.id = e.user_id ORDER BY e.id DESC LIMIT ?", limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if rows.Scan(&e.TS, &e.Kind, &e.UserID, &e.Email, &e.Detail) == nil {
			out = append(out, e)
		}
	}
	return out
}

// Stats is a point-in-time account breakdown for the activity summary line.
type Stats struct {
	Total, Free, TrialLive, TrialExpired, Active int
}

func (s *Store) Stats() Stats {
	now := time.Now().Unix()
	one := func(q string, args ...any) int {
		var n int
		s.db.QueryRow(q, args...).Scan(&n)
		return n
	}
	return Stats{
		Total:        one("SELECT COUNT(*) FROM users"),
		Free:         one("SELECT COUNT(*) FROM users WHERE plan='free'"),
		Active:       one("SELECT COUNT(*) FROM users WHERE plan='mago'"),
		TrialLive:    one("SELECT COUNT(*) FROM users WHERE plan='trial' AND trial_ends > ?", now),
		TrialExpired: one("SELECT COUNT(*) FROM users WHERE plan='trial' AND trial_ends <= ?", now),
	}
}

// FirstEvent records a stripe event id; returns false if already seen (webhook dedup).
func (s *Store) FirstEvent(id string) bool {
	if id == "" {
		return false
	}
	res, err := s.db.Exec("INSERT OR IGNORE INTO stripe_events (id, seen_at) VALUES (?, ?)", id, time.Now().Unix())
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

// --- GitHub App installations ---

type Installation struct {
	ID          int64
	AccountID   int64
	GithubLogin string
	Repos       []string
}

func reposToJSON(repos []string) string {
	sort.Strings(repos)
	b, _ := json.Marshal(repos)
	return string(b)
}

func (s *Store) installRepos(id int64) []string {
	var raw string
	if s.db.QueryRow("SELECT repos_json FROM installations WHERE installation_id = ?", id).Scan(&raw) != nil {
		return nil
	}
	var repos []string
	json.Unmarshal([]byte(raw), &repos)
	return repos
}

// UpsertInstallation records an installation (from an `installation` webhook), replacing its
// repo set but PRESERVING any account_id a prior `mago link` set.
func (s *Store) UpsertInstallation(id int64, login string, repos []string) error {
	_, err := s.db.Exec(`
INSERT INTO installations (installation_id, github_login, repos_json, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(installation_id) DO UPDATE SET github_login=excluded.github_login, repos_json=excluded.repos_json, updated_at=excluded.updated_at`,
		id, login, reposToJSON(repos), time.Now().Unix())
	return err
}

// MutateInstallationRepos applies add/remove deltas (from `installation_repositories`),
// creating the row unclaimed if we somehow missed the `installation` created event.
func (s *Store) MutateInstallationRepos(id int64, login string, add, remove []string) error {
	set := map[string]bool{}
	for _, r := range s.installRepos(id) {
		set[r] = true
	}
	for _, r := range add {
		set[r] = true
	}
	for _, r := range remove {
		delete(set, r)
	}
	repos := make([]string, 0, len(set))
	for r := range set {
		repos = append(repos, r)
	}
	return s.UpsertInstallation(id, login, repos)
}

func (s *Store) DeleteInstallation(id int64) error {
	_, err := s.db.Exec("DELETE FROM installations WHERE installation_id = ?", id)
	return err
}

// ClaimInstallation binds an installation to a mago account. Errors if the installation is
// unknown (the App must be installed first, so its webhook has registered it).
func (s *Store) ClaimInstallation(id, accountID int64) error {
	res, err := s.db.Exec("UPDATE installations SET account_id=?, updated_at=? WHERE installation_id=?",
		accountID, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("installation %d not found — install the GitHub App on your repos first", id)
	}
	return nil
}

// EntitledRepos is the set of repos an account may receive relayed events for: the union of
// repos across every installation it has claimed (GitHub App path) plus its direct repo_grants
// (operator-provisioned webhook path).
func (s *Store) EntitledRepos(accountID int64) map[string]bool {
	out := map[string]bool{}
	if rows, err := s.db.Query("SELECT repos_json FROM installations WHERE account_id = ?", accountID); err == nil {
		for rows.Next() {
			var raw string
			if rows.Scan(&raw) == nil {
				var repos []string
				json.Unmarshal([]byte(raw), &repos)
				for _, r := range repos {
					out[r] = true
				}
			}
		}
		rows.Close()
	}
	if rows, err := s.db.Query("SELECT repo FROM repo_grants WHERE account_id = ?", accountID); err == nil {
		for rows.Next() {
			var repo string
			if rows.Scan(&repo) == nil {
				out[repo] = true
			}
		}
		rows.Close()
	}
	return out
}

// GrantRepo entitles an account to a repo (operator-provisioned webhook path).
func (s *Store) GrantRepo(accountID int64, repo string) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO repo_grants (account_id, repo) VALUES (?, ?)", accountID, repo)
	return err
}

func (s *Store) RevokeRepo(accountID int64, repo string) error {
	_, err := s.db.Exec("DELETE FROM repo_grants WHERE account_id = ? AND repo = ?", accountID, repo)
	return err
}

func (s *Store) InstallationsForAccount(accountID int64) []Installation {
	var out []Installation
	rows, err := s.db.Query("SELECT installation_id, account_id, github_login, repos_json FROM installations WHERE account_id = ? ORDER BY installation_id", accountID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var in Installation
		var raw string
		if rows.Scan(&in.ID, &in.AccountID, &in.GithubLogin, &raw) != nil {
			continue
		}
		json.Unmarshal([]byte(raw), &in.Repos)
		out = append(out, in)
	}
	return out
}
