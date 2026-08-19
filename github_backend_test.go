package main

import (
	"encoding/json"
	"testing"
)

func TestIssueNumberFromURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://github.com/owner/repo/issues/42", "42"},
		{"https://github.com/owner/repo/issues/7#comment", "7#comment"},
		{"123", "123"},
		{"", ""},
	}
	for _, c := range cases {
		got := issueNumberFromURL(c.in)
		if got != c.want {
			t.Errorf("issueNumberFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsMagoComment(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"🔧 agent picking this up", true},
		{"🙋 **cto** needs the CEO:", true},
		{"📋 routed to cto", true},
		{"↩ Not the right role", true},
		{"📣 shipped", true},
		{"**agent** _(mago agent)_", true},
		{"human reply here", false},
		{"  **bold** start with spaces", true},
		{"", false},
	}
	for _, c := range cases {
		got := isMagoComment(c.in)
		if got != c.want {
			t.Errorf("isMagoComment(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestGhIssueMethods(t *testing.T) {
	payload := []byte(`{"number":1,"title":"test","state":"open","labels":[{"name":"mago:in-progress"},{"name":"agent:cto"},{"name":"project:supercli"}]}`)
	var gi ghIssue
	if err := json.Unmarshal(payload, &gi); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !gi.hasLabel("mago:in-progress") {
		t.Error("hasLabel: expected to find mago:in-progress")
	}
	if gi.hasLabel("mago:blocked") {
		t.Error("hasLabel: unexpected mago:blocked")
	}
	if gi.assignee() != "cto" {
		t.Errorf("assignee() = %q, want cto", gi.assignee())
	}
	if gi.project() != "supercli" {
		t.Errorf("project() = %q, want supercli", gi.project())
	}
	if gi.status() != "in_progress" {
		t.Errorf("status() = %q, want in_progress", gi.status())
	}
}
