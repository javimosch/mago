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
