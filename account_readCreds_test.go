package main

import (
	"testing"
)

// TestReadCreds_EmailFlagOverridesKnown verifies that an explicit --email flag
// takes precedence over the knownEmail fallback and still resolves the password
// from $MAGO_PASSWORD.
func TestReadCreds_EmailFlagOverridesKnown(t *testing.T) {
	t.Setenv("MAGO_PASSWORD", "envpass")
	email, password, err := readCreds([]string{"--email", "  FLAG@EXAMPLE.COM  "}, "known@example.com")
	if err != nil {
		t.Fatalf("readCreds: %v", err)
	}
	if email != "flag@example.com" {
		t.Errorf("email = %q, want flag@example.com", email)
	}
	if password != "envpass" {
		t.Errorf("password = %q, want envpass", password)
	}
}
