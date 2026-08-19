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
