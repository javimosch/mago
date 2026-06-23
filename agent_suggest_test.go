package main

import (
	"strings"
	"testing"
)

func TestAgentNameDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"cto", "cto", 0},
		{"cto", "ctoo", 1},
		{"cto", "cta", 1},
		{"cto", "ct", 1},
		{"kitten", "sitting", 3},
		{"café", "cafe", 1}, // rune-wise: one substitution, not two bytes
	}
	for _, tc := range cases {
		if got := agentNameDistance(tc.a, tc.b); got != tc.want {
			t.Errorf("agentNameDistance(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		// distance is symmetric
		if got := agentNameDistance(tc.b, tc.a); got != tc.want {
			t.Errorf("agentNameDistance(%q,%q) = %d, want %d (symmetry)", tc.b, tc.a, got, tc.want)
		}
	}
}

func TestNearestAgentName(t *testing.T) {
	agents := []string{"cto", "head-of-product", "head-of-org-engineering"}
	cases := []struct {
		input string
		want  string
	}{
		{"cto", "cto"},                         // exact (also prefix)
		{"ctoo", "cto"},                        // one edit
		{"ct", "cto"},                          // unique prefix
		{"CTO", "cto"},                         // case-insensitive
		{"  cto  ", "cto"},                     // trimmed
		{"head-of-product", "head-of-product"}, // exact (unique prefix)
		{"xyz", ""},                            // nothing close
		{"", ""},                               // empty input
		{"planner", ""},                        // unrelated, too far
	}
	for _, tc := range cases {
		if got := nearestAgentName(tc.input, agents); got != tc.want {
			t.Errorf("nearestAgentName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNearestAgentNameAmbiguousPrefix(t *testing.T) {
	// Two candidates share the prefix "head-of-" — a bare "head-of-" prefix is
	// ambiguous, so no single prefix winner; fall back to edit distance, which is
	// too far for either full name -> no suggestion.
	agents := []string{"head-of-product", "head-of-engineering"}
	if got := nearestAgentName("head-of-", agents); got != "" {
		t.Errorf("ambiguous prefix should not suggest, got %q", got)
	}
}

func TestUnknownAgentErrorListsAndSuggests(t *testing.T) {
	agents := []string{"head-of-product", "cto", "head-of-org-engineering"}
	err := unknownAgentError("ctoo", "/co/.mago/agents", agents)
	msg := err.Error()
	// keeps the name and directory (existing callers/tests depend on these)
	if !strings.Contains(msg, "ctoo") {
		t.Errorf("error should name the agent, got: %v", msg)
	}
	if !strings.Contains(msg, "/co/.mago/agents") {
		t.Errorf("error should name the agents dir, got: %v", msg)
	}
	// suggests the nearest match
	if !strings.Contains(msg, `did you mean "cto"`) {
		t.Errorf("error should suggest the nearest agent, got: %v", msg)
	}
	// lists all available agents, sorted
	if !strings.Contains(msg, "available agents:") {
		t.Errorf("error should list available agents, got: %v", msg)
	}
	for _, a := range agents {
		if !strings.Contains(msg, a) {
			t.Errorf("error should list agent %q, got: %v", a, msg)
		}
	}
	// sorted order: cto before head-of-org-engineering before head-of-product
	idxCto := strings.Index(msg, "available agents:")
	list := msg[idxCto:]
	if strings.Index(list, "cto") > strings.Index(list, "head-of-product") {
		t.Errorf("available agents should be sorted, got: %v", list)
	}
}

func TestUnknownAgentErrorNoSuggestionWhenFar(t *testing.T) {
	agents := []string{"cto", "head-of-product"}
	msg := unknownAgentError("zzzzzzzz", "/d", agents).Error()
	if strings.Contains(msg, "did you mean") {
		t.Errorf("unrelated input should not get a suggestion, got: %v", msg)
	}
	if !strings.Contains(msg, "available agents:") {
		t.Errorf("error should still list available agents, got: %v", msg)
	}
}

func TestUnknownAgentErrorNoAgents(t *testing.T) {
	msg := unknownAgentError("cto", "/co/.mago/agents", nil).Error()
	if !strings.Contains(msg, "cto") || !strings.Contains(msg, "/co/.mago/agents") {
		t.Errorf("error should still name agent and dir, got: %v", msg)
	}
	if !strings.Contains(msg, "mago init") {
		t.Errorf("with no agents the error should point at `mago init`, got: %v", msg)
	}
	if strings.Contains(msg, "did you mean") {
		t.Errorf("no agents -> no suggestion, got: %v", msg)
	}
}

// TestLoadAgentUnknownNameSuggests verifies the enriched error flows through
// loadAgent (the path mago run/loop hit on an unknown agent name).
func TestLoadAgentUnknownNameSuggests(t *testing.T) {
	c := newTestCompany(t)
	writeAgentFile(t, c, "cto", "You build things.\n")
	writeAgentFile(t, c, "head-of-product", "You plan things.\n")
	_, err := c.loadAgent("ctoo")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
	msg := err.Error()
	if !strings.Contains(msg, `did you mean "cto"`) {
		t.Errorf("loadAgent should suggest nearest agent, got: %v", msg)
	}
	if !strings.Contains(msg, "available agents:") {
		t.Errorf("loadAgent should list available agents, got: %v", msg)
	}
}
