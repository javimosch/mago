package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAgent(t *testing.T, dir, name, body string) {
	t.Helper()
	agentsDir := filepath.Join(dir, ".mago", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMarketingAgentNone(t *testing.T) {
	c := &Company{Dir: t.TempDir()}
	if got := c.marketingAgent(); got != nil {
		t.Fatalf("marketingAgent() = %v, want nil", got)
	}
}

func TestMarketingAgentSkipsEngineering(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "cto", "---\nname: cto\nimplements: true\n---\n")
	c := &Company{Dir: dir}
	if got := c.marketingAgent(); got != nil {
		t.Fatalf("marketingAgent() = %v, want nil", got)
	}
}

func TestMarketingAgentFindsCMO(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "cmo", "---\nname: cmo\ntitle: Chief Marketing Officer\nprovider: tau\n---\n")
	c := &Company{Dir: dir}
	got := c.marketingAgent()
	if got == nil {
		t.Fatal("marketingAgent() = nil, want CMO")
	}
	if got.Name != "cmo" || got.Title != "Chief Marketing Officer" {
		t.Fatalf("marketingAgent() = %q/%q, want cmo/Chief Marketing Officer", got.Name, got.Title)
	}
}

func TestMarketingAgentMixed(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "cto", "---\nname: cto\nimplements: true\n---\n")
	writeAgent(t, dir, "cmo", "---\nname: cmo\n---\n")
	c := &Company{Dir: dir}
	got := c.marketingAgent()
	if got == nil || got.Name != "cmo" {
		t.Fatalf("marketingAgent() = %v, want cmo", got)
	}
}

func TestShipReleaseNoteNoCMO(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "cto", "---\nname: cto\nimplements: true\n---\n")
	c := &Company{Dir: dir}
	if c.shipReleaseNote("owner/repo", 1, "a title") {
		t.Fatal("shipReleaseNote(...) = true, want false with no CMO")
	}
}

// TestShipReleaseNote_Posts verifies the CMO can draft a release note and the comment is
// posted on the merged PR when both the harness and gh succeed.
func TestShipReleaseNote_Posts(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\ncat >/dev/null 2>/dev/null\necho '{\"result\":\"Shipped a thing\",\"is_error\":false,\"subtype\":\"\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("MAGO_PROVIDER", "")
	t.Setenv("MAGO_MODEL", "")

	dir := t.TempDir()
	writeAgent(t, dir, "cmo", "---\nname: cmo\ntitle: Chief Marketing Officer\nprovider: claude\n---\n")
	c := &Company{Dir: dir}
	if !c.shipReleaseNote("owner/repo", 42, "a title") {
		t.Fatal("shipReleaseNote(...) = false, want true")
	}
}

// TestShipReleaseNote_EmptyNote verifies the CMO failing to produce a non-empty note
// (empty/whitespace content) is reported as a failure before any gh call.
func TestShipReleaseNote_EmptyNote(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\ncat >/dev/null 2>/dev/null\necho '{\"result\":\"   \",\"is_error\":false,\"subtype\":\"\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("MAGO_PROVIDER", "")
	t.Setenv("MAGO_MODEL", "")

	dir := t.TempDir()
	writeAgent(t, dir, "cmo", "---\nname: cmo\ntitle: Chief Marketing Officer\nprovider: claude\n---\n")
	c := &Company{Dir: dir}
	if c.shipReleaseNote("owner/repo", 42, "a title") {
		t.Fatal("shipReleaseNote(...) = true, want false for empty note")
	}
}

// TestShipReleaseNote_GhFails verifies the CMO is reported as failing when the release
// note is drafted but the `gh pr comment` call fails.
func TestShipReleaseNote_GhFails(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\ncat >/dev/null 2>/dev/null\necho '{\"result\":\"Shipped a thing\",\"is_error\":false,\"subtype\":\"\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Setenv("MAGO_PROVIDER", "")
	t.Setenv("MAGO_MODEL", "")

	dir := t.TempDir()
	writeAgent(t, dir, "cmo", "---\nname: cmo\ntitle: Chief Marketing Officer\nprovider: claude\n---\n")
	c := &Company{Dir: dir}
	if c.shipReleaseNote("owner/repo", 42, "a title") {
		t.Fatal("shipReleaseNote(...) = true, want false when gh fails")
	}
}

func TestMarketingAgentSkipsReviewerAndPlanner(t *testing.T) {
	dir := t.TempDir()
	writeAgent(t, dir, "critic", "---\nname: critic\nreviews: true\nplans: true\n---\n")
	writeAgent(t, dir, "cmo", "---\nname: cmo\n---\n")
	c := &Company{Dir: dir}
	got := c.marketingAgent()
	if got == nil || got.Name != "cmo" {
		t.Fatalf("marketingAgent() = %v, want cmo", got)
	}
}

func TestAgentCompleteClaude(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")
	body := "#!/bin/sh\ncat >/dev/null 2>/dev/null\necho '{\"result\": \"done\", \"is_error\": false, \"subtype\": \"\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	got, err := agentComplete(&Agent{Provider: "claude"}, "do the thing")
	if err != nil {
		t.Fatalf("agentComplete: %v", err)
	}
	if got != "done" {
		t.Errorf("agentComplete = %q, want done", got)
	}
}

func TestAgentCompleteDispatch(t *testing.T) {
	bin := t.TempDir()
	scripts := []struct {
		name string
		body string
	}{
		{
			"claude",
			"#!/bin/sh\ncat >/dev/null 2>/dev/null\necho '{\"result\": \"claude\", \"is_error\": false, \"subtype\": \"\"}'\n",
		},
		{
			"tau",
			"#!/bin/sh\necho '{\"content\": \"tau\"}'\n",
		},
		{
			"pi",
			"#!/bin/sh\necho '{\"type\": \"turn_end\", \"message\": {\"role\": \"assistant\", \"content\": [{\"type\": \"text\", \"text\": \"pi\"}]}}'\n",
		},
		{
			"debri",
			"#!/bin/sh\necho '{\"content\": \"debri\"}'\n",
		},
	}
	for _, s := range scripts {
		if err := os.WriteFile(filepath.Join(bin, s.name), []byte(s.body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))

	cases := []struct {
		provider string
		want     string
	}{
		{"claude", "claude"},
		{"tau", "tau"},
		{"pi", "pi"},
		{"debri", "debri"},
		{"", "tau"}, // default harness is tau
	}
	for _, tc := range cases {
		got, err := agentComplete(&Agent{Provider: tc.provider}, "prompt")
		if err != nil {
			t.Fatalf("agentComplete(%q): %v", tc.provider, err)
		}
		if got != tc.want {
			t.Errorf("agentComplete(%q) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}
