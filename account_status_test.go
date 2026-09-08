package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdAccount_FetchError verifies that cmdAccount surfaces an error when the
// platform account lookup fails, rather than printing stale or empty status.
func TestCmdAccount_FetchError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".mago"), 0o755); err != nil {
		t.Fatalf("setup .mago dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".mago", "config.json"), []byte(`{"token":"token"}`), 0o600); err != nil {
		t.Fatalf("setup config: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/account" || r.Method != "GET" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("MAGO_PLATFORM_URL", srv.URL)

	err := cmdAccount([]string{"status"})
	if err == nil {
		t.Fatal("cmdAccount: expected error for failed account lookup")
	}
	if !strings.Contains(err.Error(), "platform returned 500") {
		t.Errorf("error = %q, want 'platform returned 500'", err.Error())
	}
}
