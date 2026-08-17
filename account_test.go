package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func tempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func TestConfigPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	want := filepath.Join(dir, ".mago", "config.json")
	if got := configPath(); got != want {
		t.Errorf("configPath() = %q, want %q", got, want)
	}
}

func TestLoadAndSaveConfig(t *testing.T) {
	_ = tempHome(t)

	// Missing config falls back to the default platform URL.
	cfg := loadConfig()
	if cfg.PlatformURL != defaultPlatformURL {
		t.Fatalf("want default URL %q, got %q", defaultPlatformURL, cfg.PlatformURL)
	}

	// Save and reload.
	cfg.Email = "ceo@example.com"
	cfg.Token = "jwt-123"
	cfg.LicenseKey = "license-456"
	if err := cfg.save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(configPath())
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("want mode 0600, got %o", info.Mode().Perm())
	}

	reloaded := loadConfig()
	if reloaded.Email != cfg.Email || reloaded.Token != cfg.Token || reloaded.LicenseKey != cfg.LicenseKey {
		t.Fatalf("reloaded config mismatch: %+v", reloaded)
	}
}

func TestLoadConfigEnvOverride(t *testing.T) {
	_ = tempHome(t)
	if err := os.MkdirAll(filepath.Dir(configPath()), 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	if err := os.WriteFile(configPath(), []byte(`{"platform_url":"http://from.file"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("MAGO_PLATFORM_URL", "http://from.env")
	cfg := loadConfig()
	if cfg.PlatformURL != "http://from.env" {
		t.Fatalf("want env URL, got %q", cfg.PlatformURL)
	}
}

func TestPlatformDo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			if r.Method != http.MethodPost {
				http.Error(w, "wrong method", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token":"abc"}`)
		case "/error":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"bad request"}`)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &cliConfig{PlatformURL: server.URL}

	var out struct {
		Token string `json:"token"`
	}
	if err := cfg.platformDo("POST", "/ok", map[string]string{"x": "y"}, false, &out); err != nil {
		t.Fatalf("platformDo ok: %v", err)
	}
	if out.Token != "abc" {
		t.Fatalf("want token abc, got %q", out.Token)
	}

	if err := cfg.platformDo("GET", "/error", nil, false, nil); err == nil || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("want bad request error, got %v", err)
	}

	if err := cfg.platformDo("GET", "/authed", nil, true, nil); err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("want not logged in error, got %v", err)
	}
}

func TestFetchAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/account" || r.Method != http.MethodGet {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer jwt-xyz" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"email":"ceo@example.com","plan":"founding","active":true,"license_key":"lic-789"}`)
	}))
	defer server.Close()

	_ = tempHome(t)
	cfg := &cliConfig{PlatformURL: server.URL, Email: "ceo@example.com", Token: "jwt-xyz"}
	acc, err := fetchAccount(cfg)
	if err != nil {
		t.Fatalf("fetchAccount: %v", err)
	}
	if acc.Plan != "founding" || !acc.Active || acc.LicenseKey != "lic-789" {
		t.Fatalf("unexpected account: %+v", acc)
	}

	// The license key was cached back to disk.
	reloaded := loadConfig()
	if reloaded.LicenseKey != "lic-789" {
		t.Fatalf("want cached license lic-789, got %q", reloaded.LicenseKey)
	}

	// Calling again with the same license is a no-op.
	if _, err := fetchAccount(cfg); err != nil {
		t.Fatalf("fetchAccount second call: %v", err)
	}
}

func TestReadCreds(t *testing.T) {
	cases := []struct {
		name      string
		rest      []string
		known     string
		wantEmail string
		wantPass  string
	}{
		{
			name:      "both flags",
			rest:      []string{"--email", "A@B.com", "--password", "pw"},
			wantEmail: "a@b.com",
			wantPass:  "pw",
		},
		{
			name:      "known email with password flag",
			rest:      []string{"--password", "secret"},
			known:     "CEO@Example.com",
			wantEmail: "ceo@example.com",
			wantPass:  "secret",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			email, pass, err := readCreds(c.rest, c.known)
			if err != nil {
				t.Fatalf("readCreds: %v", err)
			}
			if email != c.wantEmail || pass != c.wantPass {
				t.Fatalf("got email=%q pass=%q, want email=%q pass=%q", email, pass, c.wantEmail, c.wantPass)
			}
		})
	}
}

func TestAtoiSafe64(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"123", 123},
		{"  456  ", 456},
		{"abc", 0},
		{"12x34", 0},
		{"", 0},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := atoiSafe64(c.in); got != c.want {
				t.Errorf("atoiSafe64(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestTrialRemaining(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		ends int64
		want string
	}{
		{"expired", now.Add(-time.Hour).Unix(), "expired"},
		{"hours left", now.Add(2 * time.Hour).Unix(), "h left"},
		{"minutes left", now.Add(30 * time.Minute).Unix(), "m left"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := trialRemaining(c.ends)
			if !strings.Contains(got, c.want) {
				t.Errorf("trialRemaining(%d) = %q, want it to contain %q", c.ends, got, c.want)
			}
		})
	}
}

func BenchmarkAtoiSafe64(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = atoiSafe64(strconv.Itoa(i))
	}
}
