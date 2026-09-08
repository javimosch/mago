package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdLink_MissingToken verifies that cmdLink requires a logged-in account.
func TestCmdLink_MissingToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".mago"), 0o755); err != nil {
		t.Fatalf("setup .mago dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".mago", "config.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("setup config: %v", err)
	}

	err := cmdLink(nil)
	if err == nil {
		t.Fatal("cmdLink: expected error when not logged in")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error = %q, want 'not logged in'", err.Error())
	}
}

// TestCmdLink_PlatformError verifies that cmdLink surfaces platform errors
// instead of printing stale or empty installation data.
func TestCmdLink_PlatformError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/installations" || r.Method != "GET" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, `{"error":"link failed"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	t.Setenv("MAGO_PLATFORM_URL", srv.URL)
	if err := os.MkdirAll(filepath.Join(home, ".mago"), 0o755); err != nil {
		t.Fatalf("setup .mago dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".mago", "config.json"), []byte(`{"token":"token"}`), 0o600); err != nil {
		t.Fatalf("setup config: %v", err)
	}

	err := cmdLink(nil)
	if err == nil {
		t.Fatal("cmdLink: expected error for failed platform call")
	}
	if !strings.Contains(err.Error(), "link failed") {
		t.Errorf("error = %q, want 'link failed'", err.Error())
	}
}

// TestCmdLink_InstallationError verifies that cmdLink surfaces a platform error
// when claiming an installation via POST.
func TestCmdLink_InstallationError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/installations" || r.Method != "POST" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, `{"error":"installation denied"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	t.Setenv("MAGO_PLATFORM_URL", srv.URL)
	if err := os.MkdirAll(filepath.Join(home, ".mago"), 0o755); err != nil {
		t.Fatalf("setup .mago dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".mago", "config.json"), []byte(`{"token":"token"}`), 0o600); err != nil {
		t.Fatalf("setup config: %v", err)
	}

	err := cmdLink([]string{"--installation", "42"})
	if err == nil {
		t.Fatal("cmdLink: expected error for failed POST")
	}
	if !strings.Contains(err.Error(), "installation denied") {
		t.Errorf("error = %q, want 'installation denied'", err.Error())
	}
}
