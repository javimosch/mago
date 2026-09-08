package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdSubscribe_MissingToken verifies that cmdSubscribe requires a token.
func TestCmdSubscribe_MissingToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".mago"), 0o755); err != nil {
		t.Fatalf("setup .mago dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".mago", "config.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("setup config: %v", err)
	}

	err := cmdSubscribe(nil)
	if err == nil {
		t.Fatal("cmdSubscribe: expected error when not logged in")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error = %q, want 'not logged in'", err.Error())
	}
}

// TestCmdBilling_MissingToken verifies that cmdBilling requires a token.
func TestCmdBilling_MissingToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".mago"), 0o755); err != nil {
		t.Fatalf("setup .mago dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".mago", "config.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("setup config: %v", err)
	}

	err := cmdBilling(nil)
	if err == nil {
		t.Fatal("cmdBilling: expected error when not logged in")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error = %q, want 'not logged in'", err.Error())
	}
}
