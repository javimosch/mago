package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAccountReadCreds(t *testing.T) {
	t.Run("flags", func(t *testing.T) {
		email, password, err := readCreds([]string{"--email", "Dev@Example.COM", "--password", "secret"}, "")
		if err != nil {
			t.Fatalf("readCreds: %v", err)
		}
		if email != "dev@example.com" {
			t.Errorf("email = %q, want lowercased", email)
		}
		_ = password
	})

	t.Run("known email keeps existing", func(t *testing.T) {
		email, password, err := readCreds([]string{"--password", "secret"}, "existing@example.com")
		if err != nil {
			t.Fatalf("readCreds: %v", err)
		}
		if email != "existing@example.com" {
			t.Errorf("email = %q, want existing@example.com", email)
		}
		_ = password
	})

	t.Run("env password fallback", func(t *testing.T) {
		t.Setenv("MAGO_PASSWORD", "envpass")
		_, password, err := readCreds([]string{"--email", "dev@example.com"}, "")
		if err != nil {
			t.Fatalf("readCreds: %v", err)
		}
		if password != "envpass" {
			t.Errorf("password = %q, want envpass", password)
		}
	})

	t.Run("password arg overrides env", func(t *testing.T) {
		t.Setenv("MAGO_PASSWORD", "envpass")
		_, password, err := readCreds([]string{"--email", "dev@example.com", "--password", "argpass"}, "")
		if err != nil {
			t.Fatalf("readCreds: %v", err)
		}
		if password != "argpass" {
			t.Errorf("password = %q, want argpass", password)
		}
	})
}

func TestAccountFetchAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/account" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
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
			LicenseKey: "license-key-123",
		})
	}))
	defer srv.Close()

	cfg := &cliConfig{PlatformURL: srv.URL, Token: "tok"}
	acc, err := fetchAccount(cfg)
	if err != nil {
		t.Fatalf("fetchAccount: %v", err)
	}
	if acc.Email != "dev@example.com" {
		t.Errorf("email = %q", acc.Email)
	}
	if acc.LicenseKey != "license-key-123" {
		t.Errorf("license = %q", acc.LicenseKey)
	}
	if cfg.LicenseKey != "license-key-123" {
		t.Errorf("cfg.LicenseKey = %q", cfg.LicenseKey)
	}

	// license should be cached in config
	b, err := os.ReadFile(filepath.Join(home, ".mago", "config.json"))
	if err != nil {
		t.Fatalf("ReadFile config: %v", err)
	}
	if !strings.Contains(string(b), "license-key-123") {
		t.Errorf("config missing license: %s", b)
	}
}
