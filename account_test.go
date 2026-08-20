package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadCreds(t *testing.T) {
	t.Setenv("MAGO_PASSWORD", "")

	email, password, err := readCreds([]string{"--password", "secret"}, "dev@example.com")
	if err != nil {
		t.Fatalf("flag password: %v", err)
	}
	if email != "dev@example.com" || password != "secret" {
		t.Errorf("flag password = %q / %q, want dev@example.com / secret", email, password)
	}

	email, password, err = readCreds([]string{"--email", "  DEV@EXAMPLE.COM  ", "--password", "p"}, "")
	if err != nil {
		t.Fatalf("all flags: %v", err)
	}
	if email != "dev@example.com" || password != "p" {
		t.Errorf("all flags = %q / %q, want dev@example.com / p", email, password)
	}

	t.Setenv("MAGO_PASSWORD", "envpass")
	email, password, err = readCreds(nil, "env@example.com")
	if err != nil {
		t.Fatalf("env password: %v", err)
	}
	if email != "env@example.com" || password != "envpass" {
		t.Errorf("env password = %q / %q, want env@example.com / envpass", email, password)
	}

	// Missing password with no env falls through to the interactive prompt; in tests stdin
	// is not a tty so prompt returns an error instead of blocking.
	t.Setenv("MAGO_PASSWORD", "")
	_, _, err = readCreds([]string{"--email", "dev@example.com"}, "")
	if err == nil {
		t.Errorf("missing password: expected error, got nil")
	}
}

func TestFetchAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/account" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(accountInfo{
			Email:      "dev@example.com",
			Plan:       "founding",
			Active:     true,
			Trial:      false,
			TrialEnds:  0,
			LicenseKey: "license-123",
		})
	}))
	defer srv.Close()

	cfg := &cliConfig{PlatformURL: srv.URL, Token: "token"}
	acc, err := fetchAccount(cfg)
	if err != nil {
		t.Fatalf("fetchAccount: %v", err)
	}
	if acc.Email != "dev@example.com" || acc.Plan != "founding" || !acc.Active || acc.LicenseKey != "license-123" {
		t.Errorf("fetchAccount result wrong: %+v", acc)
	}
	if cfg.LicenseKey != "license-123" {
		t.Errorf("cfg.LicenseKey = %q, want license-123", cfg.LicenseKey)
	}

	// The cached license should be persisted to disk.
	c2 := loadConfig()
	if c2.LicenseKey != "license-123" {
		t.Errorf("persisted LicenseKey = %q, want license-123", c2.LicenseKey)
	}

	// An unauthenticated request should return an error.
	cfg.Token = "bad"
	if _, err := fetchAccount(cfg); err == nil {
		t.Errorf("fetchAccount bad token: expected error")
	}
}

func TestAtoiSafe64(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"42", 42},
		{"  123  ", 123},
		{"12abc", 0},
		{"", 0},
		{"-5", 0},
	}
	for _, tc := range cases {
		if got := atoiSafe64(tc.in); got != tc.want {
			t.Errorf("atoiSafe64(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestTrialRemaining(t *testing.T) {
	futureHours := time.Now().Add(2*time.Hour + 15*time.Minute).Unix()
	if got := trialRemaining(futureHours); !strings.Contains(got, "h left") {
		t.Errorf("trialRemaining(future hours) = %q, want 'h left'", got)
	}

	futureMins := time.Now().Add(30 * time.Minute).Unix()
	if got := trialRemaining(futureMins); !strings.Contains(got, "m left") {
		t.Errorf("trialRemaining(future minutes) = %q, want 'm left'", got)
	}

	past := time.Now().Add(-time.Hour).Unix()
	if got := trialRemaining(past); !strings.Contains(got, "expired") {
		t.Errorf("trialRemaining(past) = %q, want 'expired'", got)
	}
}

func TestLoadConfigAndSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MAGO_PLATFORM_URL", "")

	// Empty home returns the default platform URL.
	c := loadConfig()
	if c.PlatformURL != defaultPlatformURL {
		t.Errorf("default PlatformURL = %q, want %q", c.PlatformURL, defaultPlatformURL)
	}

	c.PlatformURL = "http://custom.example"
	c.Email = "dev@example.com"
	c.Token = "tok"
	c.LicenseKey = "key"
	if err := c.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Environment wins over the saved file.
	t.Setenv("MAGO_PLATFORM_URL", "http://env.example")
	c2 := loadConfig()
	if c2.PlatformURL != "http://env.example" {
		t.Errorf("env override = %q, want %q", c2.PlatformURL, "http://env.example")
	}

	t.Setenv("MAGO_PLATFORM_URL", "")
	c3 := loadConfig()
	if c3.Email != "dev@example.com" || c3.Token != "tok" || c3.LicenseKey != "key" {
		t.Errorf("reload config wrong: %+v", c3)
	}

	want := filepath.Join(home, ".mago", "config.json")
	if got := configPath(); got != want {
		t.Errorf("configPath() = %q, want %q", got, want)
	}
}

func TestCliConfigPlatformDo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test":
			if r.Method != "POST" {
				http.Error(w, "wrong method", http.StatusMethodNotAllowed)
				return
			}
			var in map[string]string
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if in["hello"] != "world" {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"reply": "ok"})
		case "/me":
			auth := r.Header.Get("Authorization")
			if auth != "Bearer secret-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"email": "dev@example.com"})
		case "/error":
			http.Error(w, `{"error":"boom"}`, http.StatusBadRequest)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := &cliConfig{PlatformURL: srv.URL}
	var out map[string]string
	if err := cfg.platformDo("POST", "/test", map[string]string{"hello": "world"}, false, &out); err != nil {
		t.Fatalf("POST /test: %v", err)
	}
	if out["reply"] != "ok" {
		t.Errorf("reply = %q, want ok", out["reply"])
	}

	cfg.Token = "secret-token"
	var me map[string]string
	if err := cfg.platformDo("GET", "/me", nil, true, &me); err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	if me["email"] != "dev@example.com" {
		t.Errorf("email = %q, want dev@example.com", me["email"])
	}

	cfg.Token = ""
	if err := cfg.platformDo("GET", "/me", nil, true, &me); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("missing token error = %v, want 'not logged in'", err)
	}

	if err := cfg.platformDo("GET", "/error", nil, false, nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("platform error response = %v, want 'boom'", err)
	}

	cfg.PlatformURL = "http://127.0.0.1:1"
	if err := cfg.platformDo("GET", "/", nil, false, nil); err == nil || !strings.Contains(err.Error(), "cannot reach platform") {
		t.Errorf("unreachable error = %v, want 'cannot reach platform'", err)
	}
}

func TestCmdLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/login" || r.Method != "POST" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var in map[string]string
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if in["email"] != "dev@example.com" || in["password"] != "secret" {
			http.Error(w, "bad creds", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": "login-token"})
	}))
	defer srv.Close()

	t.Setenv("MAGO_PLATFORM_URL", srv.URL)

	if err := cmdLogin([]string{"--email", "dev@example.com", "--password", "secret"}); err != nil {
		t.Fatalf("cmdLogin: %v", err)
	}

	cfg := loadConfig()
	if cfg.Email != "dev@example.com" {
		t.Errorf("Email = %q, want dev@example.com", cfg.Email)
	}
	if cfg.Token != "login-token" {
		t.Errorf("Token = %q, want login-token", cfg.Token)
	}
}
