package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
