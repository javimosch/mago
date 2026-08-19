package main

import "testing"

func TestClip(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 6, "hello…"},
		{"  hello\nworld  ", 12, "hello world"},
		{"", 5, ""},
		{"exactly", 7, "exactly"},
	}
	for _, c := range cases {
		got := clip(c.in, c.n)
		if got != c.want {
			t.Errorf("clip(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestOrStr(t *testing.T) {
	if got := orStr("hello", "default"); got != "hello" {
		t.Errorf("orStr(\"hello\", \"default\") = %q, want \"hello\"", got)
	}
	if got := orStr("  ", "default"); got != "default" {
		t.Errorf("orStr(\"  \", \"default\") = %q, want \"default\"", got)
	}
	if got := orStr("", "default"); got != "default" {
		t.Errorf("orStr(\"\", \"default\") = %q, want \"default\"", got)
	}
}
