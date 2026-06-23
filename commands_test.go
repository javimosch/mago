package main

import (
	"strings"
	"testing"
)

// TestParseCompanyDir_Defaults verifies the default company dir is the cwd ("." ) when
// neither -C nor $MAGO_COMPANY is given, and that positional args pass through untouched.
func TestParseCompanyDir_Defaults(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	dir, rest, err := parseCompanyDir([]string{"add", "a title"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "." {
		t.Errorf("dir = %q, want %q", dir, ".")
	}
	if strings.Join(rest, " ") != "add a title" {
		t.Errorf("rest = %v, want [add a title]", rest)
	}
}

// TestParseCompanyDir_FlagSetsDirAndStripsIt verifies -C <dir> sets the dir and removes both
// tokens from the returned positional args, regardless of position in the arg list.
func TestParseCompanyDir_FlagSetsDirAndStripsIt(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	dir, rest, err := parseCompanyDir([]string{"add", "title", "-C", "/srv/acme"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/srv/acme" {
		t.Errorf("dir = %q, want /srv/acme", dir)
	}
	if strings.Join(rest, " ") != "add title" {
		t.Errorf("rest = %v, want [add title]", rest)
	}
}

// TestParseCompanyDir_EnvDefaultOverriddenByFlag verifies $MAGO_COMPANY supplies the default
// dir but an explicit -C wins.
func TestParseCompanyDir_EnvDefaultOverriddenByFlag(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "/env/dir")

	dir, _, err := parseCompanyDir([]string{"status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/env/dir" {
		t.Errorf("dir = %q, want /env/dir (from env)", dir)
	}

	dir, _, err = parseCompanyDir([]string{"status", "-C", "/flag/dir"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/flag/dir" {
		t.Errorf("dir = %q, want /flag/dir (flag overrides env)", dir)
	}
}

// TestParseCompanyDir_BareFlagRejected verifies a -C with no following directory value (or an
// empty/whitespace value) is rejected instead of being silently swallowed as a positional arg.
func TestParseCompanyDir_BareFlagRejected(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	cases := [][]string{
		{"-C"},                // trailing bare flag
		{"status", "-C"},      // bare flag after a positional
		{"-C", ""},            // empty value
		{"-C", "   "},         // whitespace-only value
		{"task", "add", "-C"}, // bare flag at the end of a real command
	}
	for _, args := range cases {
		dir, rest, err := parseCompanyDir(args)
		if err == nil {
			t.Errorf("parseCompanyDir(%v): expected error, got dir=%q rest=%v", args, dir, rest)
			continue
		}
		if !strings.Contains(err.Error(), "-C") {
			t.Errorf("parseCompanyDir(%v): error %q should mention -C", args, err)
		}
	}
}

// TestLoadCompany_MissingCompanyNamesRemedies verifies the not-a-mago-company error points the
// user at both remedies: the -C <dir> flag and the $MAGO_COMPANY env var.
func TestLoadCompany_MissingCompanyNamesRemedies(t *testing.T) {
	dir := t.TempDir() // empty dir with no .mago/
	_, err := loadCompany(dir)
	if err == nil {
		t.Fatal("expected error for a directory without .mago/, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "-C") {
		t.Errorf("error %q should name the -C <dir> remedy", msg)
	}
	if !strings.Contains(msg, "MAGO_COMPANY") {
		t.Errorf("error %q should name the $MAGO_COMPANY remedy", msg)
	}
}
