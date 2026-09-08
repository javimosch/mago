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

// TestCmdRegister_SignupError verifies that cmdRegister surfaces a platform
// signup error instead of continuing to the onboarding flow.
func TestCmdRegister_SignupError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/signup" || r.Method != "POST" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"signup denied"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	t.Setenv("MAGO_PLATFORM_URL", srv.URL)

	err := cmdRegister([]string{"--email", "dev@example.com", "--password", "secret"})
	if err == nil {
		t.Fatal("cmdRegister: expected error for failed signup")
	}
	if !strings.Contains(err.Error(), "signup denied") {
		t.Errorf("error = %q, want 'signup denied'", err.Error())
	}
}

// TestCmdRegister_SaveError verifies that cmdRegister surfaces an error when
// the local config cannot be persisted after a successful signup.
func TestCmdRegister_SaveError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Make .mago a regular file so MkdirAll fails when saving the config.
	magoPath := filepath.Join(home, ".mago")
	if err := os.WriteFile(magoPath, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("setup .mago file: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/signup" || r.Method != "POST" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": "register-token"})
	}))
	defer srv.Close()

	t.Setenv("MAGO_PLATFORM_URL", srv.URL)

	err := cmdRegister([]string{"--email", "dev@example.com", "--password", "secret"})
	if err == nil {
		t.Fatal("cmdRegister: expected error when config cannot be saved")
	}
}
