package main

import (
	"os"
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

// TestWriteIfMissing_CreatesOnlyWhenAbsent verifies writeIfMissing writes the default
// content when the file is missing and leaves an existing file untouched.
func TestWriteIfMissing_CreatesOnlyWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/starter.txt"

	writeIfMissing(path, "default")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}
	if string(b) != "default" {
		t.Errorf("created content = %q, want %q", string(b), "default")
	}

	writeIfMissing(path, "changed")
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to remain readable: %v", err)
	}
	if string(b) != "default" {
		t.Errorf("existing content was overwritten to %q, want %q", string(b), "default")
	}
}

// TestBackfillAgentFlag_AddsMissingFlagAndNoOps verifies backfillAgentFlag injects a
// missing frontmatter flag, preserves an already-set flag, and is a no-op on missing files.
func TestBackfillAgentFlag_AddsMissingFlagAndNoOps(t *testing.T) {
	dir := t.TempDir()

	// Missing file is a no-op.
	backfillAgentFlag(dir+"/missing.md", "plans", "true")

	path := dir + "/agent.md"
	os.WriteFile(path, []byte("---\nname: tester\n---\nbody\n"), 0o644)
	backfillAgentFlag(path, "plans", "true")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "plans: true") {
		t.Errorf("updated frontmatter missing plans flag:\n%s", got)
	}

	// Already-set value should be left unchanged.
	backfillAgentFlag(path, "plans", "true")
	b2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to re-read file: %v", err)
	}
	if string(b2) != got {
		t.Errorf("re-running backfill changed the file unexpectedly")
	}
}

func TestIfStr(t *testing.T) {
	cases := []struct {
		cond     bool
		trueVal  string
		falseVal string
		want     string
	}{
		{true, "yes", "no", "yes"},
		{false, "yes", "no", "no"},
		{true, "", "fallback", ""},
		{false, "ignored", "", ""},
	}
	for _, tc := range cases {
		if got := ifStr(tc.cond, tc.trueVal, tc.falseVal); got != tc.want {
			t.Errorf("ifStr(%v, %q, %q) = %q, want %q", tc.cond, tc.trueVal, tc.falseVal, got, tc.want)
		}
	}
}
