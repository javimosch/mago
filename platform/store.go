package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type User struct {
	ID             int64  `json:"id"`
	Email          string `json:"email"`
	PasswordHash   string `json:"password_hash"`
	Plan           string `json:"plan"` // "free" | "mago"
	StripeCustomer string `json:"stripe_customer,omitempty"`
	StripeSub      string `json:"stripe_sub,omitempty"`
	LicenseKey     string `json:"license_key,omitempty"`
	CreatedAt      int64  `json:"created_at"`
}

// Store is a tiny JSON-file-backed store for the skeleton. Production swaps this for SQLite
// (schema in docs/SAAS.md) behind the same methods. Not tuned for concurrency at scale.
type Store struct {
	mu     sync.Mutex
	path   string
	Users  []*User         `json:"users"`
	Events map[string]bool `json:"events"` // stripe event ids seen (webhook dedup)
	NextID int64           `json:"next_id"`
}

func openStore(path string) (*Store, error) {
	s := &Store{path: path, Events: map[string]bool{}, NextID: 1}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, s); err != nil {
			return nil, fmt.Errorf("parse store %s: %w", path, err)
		}
		if s.Events == nil {
			s.Events = map[string]bool{}
		}
		if s.NextID == 0 {
			s.NextID = 1
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) save() {
	if b, err := json.MarshalIndent(s, "", "  "); err == nil {
		os.WriteFile(s.path, b, 0o600)
	}
}

// internal finders — call only while holding s.mu
func (s *Store) findByEmail(e string) *User {
	for _, u := range s.Users {
		if u.Email == e {
			return u
		}
	}
	return nil
}
func (s *Store) findByID(id int64) *User {
	for _, u := range s.Users {
		if u.ID == id {
			return u
		}
	}
	return nil
}
func (s *Store) findByLicense(k string) *User {
	for _, u := range s.Users {
		if k != "" && u.LicenseKey == k {
			return u
		}
	}
	return nil
}

func (s *Store) GetByEmail(e string) *User   { s.mu.Lock(); defer s.mu.Unlock(); return s.findByEmail(e) }
func (s *Store) GetByID(id int64) *User      { s.mu.Lock(); defer s.mu.Unlock(); return s.findByID(id) }
func (s *Store) GetByLicense(k string) *User { s.mu.Lock(); defer s.mu.Unlock(); return s.findByLicense(k) }

func (s *Store) Create(email, hash string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.findByEmail(email) != nil {
		return nil, fmt.Errorf("email already registered")
	}
	u := &User{ID: s.NextID, Email: email, PasswordHash: hash, Plan: "free", CreatedAt: time.Now().Unix()}
	s.NextID++
	s.Users = append(s.Users, u)
	s.save()
	return u, nil
}

// Update runs fn under the lock and persists. Use findByID inside fn.
func (s *Store) Update(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
	s.save()
}

// FirstEvent records a stripe event id; returns false if already seen (dedup).
func (s *Store) FirstEvent(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" || s.Events[id] {
		return false
	}
	s.Events[id] = true
	s.save()
	return true
}
