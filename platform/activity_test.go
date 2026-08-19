package main

import "testing"

func TestEventIcon(t *testing.T) {
	cases := []struct {
		kind, want string
	}{
		{"signup", "🆕 signup"},
		{"subscribed", "💳 subscribed"},
		{"canceled", "✖ canceled"},
		{"worker_connect", "🟢 worker_connect"},
		{"worker_disconnect", "⚪ worker_disconnect"},
		{"linked", "🔗 linked"},
		{"unknown", "unknown"},
	}
	for _, c := range cases {
		got := eventIcon(c.kind)
		if got != c.want {
			t.Errorf("eventIcon(%q) = %q, want %q", c.kind, got, c.want)
		}
	}
}
