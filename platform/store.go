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
	Plan           string // "free" | "mago"
	StripeCustomer string
	StripeSub      string
	LicenseKey     string
	CreatedAt      int64
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
  created_at      INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS stripe_events (
  id      TEXT PRIMARY KEY,
  seen_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS installations (
  installation_id INTEGER PRIMARY KEY,        -- GitHub App installation id
  account_id      INTEGER NOT NULL DEFAULT 0, -- mago user id; 0 = unclaimed (set by ` + "`mago link`" + `)
  github_login    TEXT NOT NULL DEFAULT '',   -- the org/user the app is installed on
  repos_json      TEXT NOT NULL DEFAULT '[]', -- repos this installation covers (from GitHub webhooks)
  updated_at      INTEGER NOT NULL
);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

const userCols = "id, email, password_hash, plan, stripe_customer, stripe_sub, license_key, created_at"

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Plan, &u.StripeCustomer, &u.StripeSub, &u.LicenseKey, &u.CreatedAt)
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
		"UPDATE users SET plan=?, stripe_customer=?, stripe_sub=?, license_key=? WHERE id=?",
		u.Plan, u.StripeCustomer, u.StripeSub, u.LicenseKey, u.ID); err != nil {
		return err
	}
	return tx.Commit()
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
// repos across every installation it has claimed.
func (s *Store) EntitledRepos(accountID int64) map[string]bool {
	out := map[string]bool{}
	rows, err := s.db.Query("SELECT repos_json FROM installations WHERE account_id = ?", accountID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if rows.Scan(&raw) != nil {
			continue
		}
		var repos []string
		json.Unmarshal([]byte(raw), &repos)
		for _, r := range repos {
			out[r] = true
		}
	}
	return out
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
