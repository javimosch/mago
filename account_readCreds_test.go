package main

import (
	"os"
	"strings"
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

// TestReadCreds_PromptPath verifies the interactive fallback: with neither flags nor
// $MAGO_PASSWORD, readCreds reads the missing values from stdin (one prompt per call
// here — each prompt uses a fresh bufio.Reader, so feeding one line at a time keeps
// read-ahead from starving a later prompt).
func TestReadCreds_PromptPath(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	feed := func(line string) {
		t.Helper()
		if _, err := w.WriteString(line); err != nil {
			t.Fatalf("feeding stdin: %v", err)
		}
	}

	// Email prompt: no flags and no known email -> read from stdin.
	t.Setenv("MAGO_PASSWORD", "envpass")
	feed("Prompt@Example.com\n")
	email, password, err := readCreds(nil, "")
	if err != nil {
		t.Fatalf("email prompt: %v", err)
	}
	if email != "prompt@example.com" || password != "envpass" {
		t.Errorf("got %q / %q, want prompt@example.com / envpass", email, password)
	}

	// Password prompt: email is known but no flag/env password -> hidden stdin read.
	t.Setenv("MAGO_PASSWORD", "")
	feed("  secret  \n")
	email, password, err = readCreds(nil, "dev@example.com")
	if err != nil {
		t.Fatalf("password prompt: %v", err)
	}
	if email != "dev@example.com" || password != "secret" {
		t.Errorf("got %q / %q, want dev@example.com / secret", email, password)
	}

	// A blank line at the password prompt leaves it empty -> required-field error.
	feed("\n")
	if _, _, err := readCreds(nil, "dev@example.com"); err == nil ||
		!strings.Contains(err.Error(), "required") {
		t.Errorf("blank password prompt: expected 'required' error, got %v", err)
	}
}
