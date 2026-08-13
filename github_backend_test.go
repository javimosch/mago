package main

import "testing"

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
