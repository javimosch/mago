package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestCompany sets up a minimal company directory in a temp dir for testing.
// It creates only the directory structure — no agent files or task files.
func newTestCompany(t *testing.T) *Company {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{
		".mago/agents", ".mago/skills", ".mago/runs", ".mago/inbox",
		"tasks", "workspace", "projects",
	} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("setup: mkdir %s: %v", sub, err)
		}
	}
	return &Company{Dir: dir, Name: "test"}
}

func writeAgentFile(t *testing.T, c *Company, name, content string) {
	t.Helper()
	path := filepath.Join(c.agentsDir(), name+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write agent %s: %v", name, err)
	}
}

// TestLoadCompanyMissingDotMago verifies loadCompany produces an actionable error
// when the target directory has no .mago/ subdirectory.
func TestLoadCompanyMissingDotMago(t *testing.T) {
	dir := t.TempDir()
	_, err := loadCompany(dir)
	if err == nil {
		t.Fatal("expected error for directory without .mago/")
	}
	msg := err.Error()
	if !strings.Contains(msg, ".mago") {
		t.Errorf("error should mention .mago/, got: %v", err)
	}
	if !strings.Contains(msg, "mago init") {
		t.Errorf("error should suggest `mago init`, got: %v", err)
	}
}

// TestLoadAgentMissingFile verifies loadAgent produces an actionable error
// that names both the agent and the directory where the file was expected.
func TestLoadAgentMissingFile(t *testing.T) {
	c := newTestCompany(t)
	_, err := c.loadAgent("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing agent file")
	}
	msg := err.Error()
	if !strings.Contains(msg, "nonexistent") {
		t.Errorf("error should mention the agent name, got: %v", err)
	}
	if !strings.Contains(msg, c.agentsDir()) {
		t.Errorf("error should mention the agents directory (%s), got: %v", c.agentsDir(), err)
	}
}

// TestLoadAgentUnknownFrontmatterKey verifies that a typo in a frontmatter key
// produces a clear error naming the bad key and listing valid alternatives.
func TestLoadAgentUnknownFrontmatterKey(t *testing.T) {
	c := newTestCompany(t)
	// "provder" is a misspelling of "provider"
	writeAgentFile(t, c, "myagent",
		"---\nname: myagent\ntitle: My Agent\nprovder: openai\n---\nDoes things.\n")
	_, err := c.loadAgent("myagent")
	if err == nil {
		t.Fatal("expected error for unknown frontmatter key")
	}
	msg := err.Error()
	if !strings.Contains(msg, "provder") {
		t.Errorf("error should name the unknown key, got: %v", err)
	}
	if !strings.Contains(msg, "provider") {
		t.Errorf("error should list valid keys including 'provider', got: %v", err)
	}
}

// TestLoadAgentInvalidBoolValue verifies that non-boolean values for reviews,
// plans, and implements produce clear errors showing the accepted values.
func TestLoadAgentInvalidBoolValue(t *testing.T) {
	cases := []struct{ key, val string }{
		{"reviews", "yes"},
		{"plans", "1"},
		{"implements", "on"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+tc.val, func(t *testing.T) {
			c := newTestCompany(t)
			content := "---\nname: agent\ntitle: Agent\n" + tc.key + ": " + tc.val + "\n---\nBody.\n"
			writeAgentFile(t, c, "agent", content)
			_, err := c.loadAgent("agent")
			if err == nil {
				t.Fatalf("expected error for %s: %s", tc.key, tc.val)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.key) {
				t.Errorf("error should name the invalid key, got: %v", err)
			}
			if !strings.Contains(msg, "true") || !strings.Contains(msg, "false") {
				t.Errorf("error should mention valid values (true/false), got: %v", err)
			}
		})
	}
}

// TestLoadAgentValidConfig verifies that a well-formed agent file parses
// all fields correctly without error.
func TestLoadAgentValidConfig(t *testing.T) {
	c := newTestCompany(t)
	writeAgentFile(t, c, "cto",
		"---\nname: cto\ntitle: Chief Technology Officer\nprovider: opencode-go\nmodel: gpt-4\nimplements: true\n---\nYou build things.\n")
	a, err := c.loadAgent("cto")
	if err != nil {
		t.Fatalf("unexpected error for valid config: %v", err)
	}
	if a.Title != "Chief Technology Officer" {
		t.Errorf("title: got %q, want %q", a.Title, "Chief Technology Officer")
	}
	if a.Provider != "opencode-go" {
		t.Errorf("provider: got %q, want %q", a.Provider, "opencode-go")
	}
	if a.Model != "gpt-4" {
		t.Errorf("model: got %q, want %q", a.Model, "gpt-4")
	}
	if !a.Implements {
		t.Error("implements: should be true")
	}
	if a.Reviews || a.Plans {
		t.Errorf("reviews/plans: should be false, got reviews=%v plans=%v", a.Reviews, a.Plans)
	}
}

// TestLoadAgentNoFrontmatter verifies that a plain-body agent file (no frontmatter)
// loads successfully with stdlib defaults for provider and model.
func TestLoadAgentNoFrontmatter(t *testing.T) {
	c := newTestCompany(t)
	writeAgentFile(t, c, "simple", "You are a simple agent.\n")
	a, err := c.loadAgent("simple")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Name != "simple" {
		t.Errorf("name: got %q, want %q", a.Name, "simple")
	}
	if a.Provider != "deepseek" {
		t.Errorf("provider default: got %q, want %q", a.Provider, "deepseek")
	}
	if a.Model != "deepseek-chat" {
		t.Errorf("model default: got %q, want %q", a.Model, "deepseek-chat")
	}
}

// TestValidateProjectsConfigMalformedJSON verifies that a malformed projects.json
// produces an error that names the file and suggests the fix command.
func TestValidateProjectsConfigMalformedJSON(t *testing.T) {
	c := newTestCompany(t)
	if err := os.WriteFile(c.projectsConfigFile(), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := c.validateProjectsConfig()
	if err == nil {
		t.Fatal("expected error for malformed JSON in projects config")
	}
	msg := err.Error()
	if !strings.Contains(msg, "projects") {
		t.Errorf("error should mention projects config, got: %v", err)
	}
	if !strings.Contains(msg, "mago project add") {
		t.Errorf("error should suggest fix command, got: %v", err)
	}
}

// TestValidateProjectsConfigAbsent verifies that a missing projects.json
// is treated as an empty project list rather than an error.
func TestValidateProjectsConfigAbsent(t *testing.T) {
	c := newTestCompany(t)
	// no projects.json written
	if err := c.validateProjectsConfig(); err != nil {
		t.Errorf("missing projects.json should not be an error, got: %v", err)
	}
}
