package main

import (
	"strings"
	"testing"
)

// TestProviderCheckNames_Claude verifies that the claude provider selects claude-specific
// checks (not tau/OPENCODE_API_KEY checks).
func TestProviderCheckNames_Claude(t *testing.T) {
	names := providerCheckNames("claude")
	if len(names) != 2 {
		t.Fatalf("claude provider: want 2 checks, got %d: %v", len(names), names)
	}
	for _, n := range names {
		if !strings.Contains(n, "claude") {
			t.Errorf("claude provider: unexpected non-claude check name %q", n)
		}
		if strings.Contains(n, "tau") || strings.Contains(n, "opencode") {
			t.Errorf("claude provider: unexpected tau/opencode check name %q", n)
		}
	}
}

// TestProviderCheckNames_Tau verifies that tau providers (including empty/unset) select
// tau/OPENCODE_API_KEY checks and NOT claude checks.
func TestProviderCheckNames_Tau(t *testing.T) {
	for _, prov := range []string{"", "opencode-go", "deepseek"} {
		t.Run("provider="+prov, func(t *testing.T) {
			names := providerCheckNames(prov)
			if len(names) != 2 {
				t.Fatalf("provider %q: want 2 checks, got %d: %v", prov, len(names), names)
			}
			for _, n := range names {
				if strings.Contains(n, "claude") {
					t.Errorf("provider %q: unexpected claude check name %q", prov, n)
				}
			}
			hasTau, hasOC := false, false
			for _, n := range names {
				if strings.Contains(n, "tau") {
					hasTau = true
				}
				if strings.Contains(n, "opencode") {
					hasOC = true
				}
			}
			if !hasTau {
				t.Errorf("provider %q: no tau check in %v", prov, names)
			}
			if !hasOC {
				t.Errorf("provider %q: no opencode-api-key check in %v", prov, names)
			}
		})
	}
}

// TestCmdWorker_NoArgs_ReturnsUserError verifies that `mago worker` (no subcommand)
// returns a *cliErr with code 80 (user-input error) instead of calling os.Exit directly.
func TestCmdWorker_NoArgs_ReturnsUserError(t *testing.T) {
	err := cmdWorker(nil)
	if err == nil {
		t.Fatal("cmdWorker with no args should return an error")
	}
	ce, ok := err.(*cliErr)
	if !ok {
		t.Fatalf("expected *cliErr, got %T: %v", err, err)
	}
	if ce.code != 80 {
		t.Errorf("exit code = %d, want 80 (user-input error per AGENTS.md)", ce.code)
	}
	if !strings.Contains(err.Error(), "worker") {
		t.Errorf("error %q should mention 'worker'", err.Error())
	}
}

// TestCmdWorker_UnknownSubcommand_ReturnsUserError verifies that `mago worker <unknown>`
// returns a *cliErr with code 80 and names the bad subcommand.
func TestCmdWorker_UnknownSubcommand_ReturnsUserError(t *testing.T) {
	err := cmdWorker([]string{"badcmd"})
	if err == nil {
		t.Fatal("cmdWorker with unknown subcommand should return an error")
	}
	ce, ok := err.(*cliErr)
	if !ok {
		t.Fatalf("expected *cliErr, got %T: %v", err, err)
	}
	if ce.code != 80 {
		t.Errorf("exit code = %d, want 80", ce.code)
	}
	if !strings.Contains(err.Error(), "badcmd") {
		t.Errorf("error %q should name the bad subcommand", err.Error())
	}
}

// TestCmdWorker_TypoSuggestsNearestSubcommand verifies the "did you mean" path also
// returns a *cliErr (not os.Exit) and names the suggestion in the message.
func TestCmdWorker_TypoSuggestsNearestSubcommand(t *testing.T) {
	err := cmdWorker([]string{"dctor"}) // typo of "doctor"
	if err == nil {
		t.Fatal("cmdWorker with typo should return an error")
	}
	if _, ok := err.(*cliErr); !ok {
		t.Fatalf("expected *cliErr, got %T", err)
	}
	if !strings.Contains(err.Error(), "doctor") {
		t.Errorf("error %q should suggest 'doctor'", err.Error())
	}
}

// TestCLIErrType verifies the cliErr type carries its code and message correctly.
func TestCLIErrType(t *testing.T) {
	e := &cliErr{80, "user input error"}
	if e.Error() != "user input error" {
		t.Errorf("Error() = %q, want %q", e.Error(), "user input error")
	}
	if e.code != 80 {
		t.Errorf("code = %d, want 80", e.code)
	}
}

// TestCheckClaudeOnPath_Label verifies the label is always set regardless of PATH.
func TestCheckClaudeOnPath_Label(t *testing.T) {
	c := checkClaudeOnPath()
	if c.label == "" {
		t.Error("checkClaudeOnPath: label must not be empty")
	}
	if !strings.Contains(c.label, "claude") {
		t.Errorf("checkClaudeOnPath: label should mention claude, got %q", c.label)
	}
	if !c.ok && c.hint == "" {
		t.Error("checkClaudeOnPath: hint must be non-empty on failure")
	}
}

// TestCheckTau_Label verifies the tau label is always set regardless of PATH.
func TestCheckTau_Label(t *testing.T) {
	c := checkTau()
	if c.label == "" {
		t.Error("checkTau: label must not be empty")
	}
	if !strings.Contains(c.label, "tau") {
		t.Errorf("checkTau: label should mention tau, got %q", c.label)
	}
	if !c.ok && c.hint == "" {
		t.Error("checkTau: hint must be non-empty on failure")
	}
}

// TestCheckOpenCodeAPIKey_FailWithoutKey verifies failure when the env var is unset.
func TestCheckOpenCodeAPIKey_FailWithoutKey(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	c := checkOpenCodeAPIKey()
	if c.ok {
		t.Error("checkOpenCodeAPIKey: should fail when OPENCODE_API_KEY is empty")
	}
	if c.hint == "" {
		t.Error("checkOpenCodeAPIKey: hint must be non-empty on failure")
	}
}

// TestCheckOpenCodeAPIKey_PassWithKey verifies success when the env var is set.
func TestCheckOpenCodeAPIKey_PassWithKey(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "test-key")
	c := checkOpenCodeAPIKey()
	if !c.ok {
		t.Errorf("checkOpenCodeAPIKey: should pass when OPENCODE_API_KEY is set, got hint: %s", c.hint)
	}
}

// TestCheckGHToken_FailWithoutToken verifies failure when MAGO_GH_TOKEN is unset.
func TestCheckGHToken_FailWithoutToken(t *testing.T) {
	t.Setenv("MAGO_GH_TOKEN", "")
	c := checkGHToken()
	if c.ok {
		t.Error("checkGHToken: should fail when MAGO_GH_TOKEN is empty")
	}
	if c.hint == "" {
		t.Error("checkGHToken: hint must be non-empty on failure")
	}
	if !strings.Contains(c.hint, "MAGO_GH_TOKEN") {
		t.Errorf("checkGHToken: hint should name MAGO_GH_TOKEN, got: %q", c.hint)
	}
	if !strings.Contains(c.hint, "github.com/settings/tokens") {
		t.Errorf("checkGHToken: hint should link to PAT docs, got: %q", c.hint)
	}
}

// TestCheckGHToken_PassWithToken verifies success when MAGO_GH_TOKEN is set.
func TestCheckGHToken_PassWithToken(t *testing.T) {
	t.Setenv("MAGO_GH_TOKEN", "ghp_test123")
	c := checkGHToken()
	if !c.ok {
		t.Errorf("checkGHToken: should pass when MAGO_GH_TOKEN is set, got hint: %s", c.hint)
	}
}

// TestCheckGHToken_LabelMentionsToken verifies the check label always mentions MAGO_GH_TOKEN.
func TestCheckGHToken_LabelMentionsToken(t *testing.T) {
	t.Setenv("MAGO_GH_TOKEN", "")
	c := checkGHToken()
	if !strings.Contains(c.label, "MAGO_GH_TOKEN") {
		t.Errorf("checkGHToken: label should mention MAGO_GH_TOKEN, got: %q", c.label)
	}
}

// TestCheckGhOnPath_Label verifies the gh-on-PATH check label names the GitHub CLI,
// and that a missing binary produces a non-empty hint pointing at install docs.
func TestCheckGhOnPath_Label(t *testing.T) {
	c := checkGhOnPath()
	if c.label == "" {
		t.Error("checkGhOnPath: label must not be empty")
	}
	if !strings.Contains(c.label, "gh") {
		t.Errorf("checkGhOnPath: label should mention gh, got %q", c.label)
	}
	if !strings.Contains(c.label, "GitHub CLI") {
		t.Errorf("checkGhOnPath: label should mention GitHub CLI, got %q", c.label)
	}
	if !c.ok && c.hint == "" {
		t.Error("checkGhOnPath: hint must be non-empty on failure")
	}
	if !c.ok && !strings.Contains(c.hint, "github.com") {
		t.Errorf("checkGhOnPath: failure hint should point at gh install docs, got %q", c.hint)
	}
}

// TestIsGHAuthError covers the key stderr patterns that indicate a GitHub auth failure.
func TestIsGHAuthError(t *testing.T) {
	authErrors := []string{
		"HTTP 401: Bad credentials",
		"error: 403 Forbidden",
		"You must be authenticated",
		"Authentication required",
		"You are not logged in",
		"You must log in",
	}
	for _, s := range authErrors {
		if !isGHAuthError(s) {
			t.Errorf("isGHAuthError(%q): expected true, got false", s)
		}
	}
	nonErrors := []string{
		"gh issue list",
		"no issues found",
		"repo not found",
		"",
	}
	for _, s := range nonErrors {
		if isGHAuthError(s) {
			t.Errorf("isGHAuthError(%q): expected false, got true", s)
		}
	}
}
