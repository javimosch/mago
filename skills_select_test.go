package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelectSkillsText_LLMSelect verifies selectSkillsText delegates to llmSelectSkills
// when the index has more than skillInjectAll entries, and only injects the selected
// skill bodies (skills with no SKILL.md are listed in the index but skipped).
func TestSelectSkillsText_LLMSelect(t *testing.T) {
	c := newTestCompany(t)

	// Fake tau binary that returns a JSON array with one known skill.
	bindir := t.TempDir()
	script := filepath.Join(bindir, "tau")
	body := "#!/bin/sh\necho '{\"content\":\"[\\\"skill-a\\\"]\"}'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake tau: %v", err)
	}
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))

	// Build 7 skills so the > skillInjectAll branch is exercised.
	var index strings.Builder
	for i := 0; i < 7; i++ {
		name := fmt.Sprintf("skill-%c", 'a'+i)
		index.WriteString("- " + name + " \u2014 hook " + name + "\n")
		dir := filepath.Join(c.skillsDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if name == "skill-a" {
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("body of skill-a\n"), 0o644); err != nil {
				t.Fatalf("write skill-a body: %v", err)
			}
		}
	}
	if err := os.WriteFile(c.skillsIndex(), []byte(index.String()), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	a := &Agent{Provider: "openai", Model: "gpt-4"}
	got := c.selectSkillsText(a, &Task{Title: "task", Body: "body"})

	if !strings.Contains(got, "--- skill: skill-a ---") {
		t.Errorf("expected skill-a body to be injected, got:\n%s", got)
	}
	if !strings.Contains(got, "skill-b") {
		t.Errorf("expected skill-b in the index, got:\n%s", got)
	}
	if strings.Contains(got, "--- skill: skill-b ---") {
		t.Error("did not expect skill-b body; it was not selected and has no SKILL.md")
	}
}
