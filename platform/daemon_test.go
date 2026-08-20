package main

import (
	"testing"
)

func TestParseStartFlags(t *testing.T) {
	t.Run("default port from env", func(t *testing.T) {
		t.Setenv("PORT", "8080")
		port, daemon := parseStartFlags(nil)
		if port != "8080" {
			t.Errorf("default port = %q, want 8080", port)
		}
		if daemon {
			t.Error("default daemon = true, want false")
		}
	})

	t.Run("--port flag", func(t *testing.T) {
		port, _ := parseStartFlags([]string{"--port", "3000"})
		if port != "3000" {
			t.Errorf("--port = %q, want 3000", port)
		}
	})

	t.Run("--port= flag", func(t *testing.T) {
		port, _ := parseStartFlags([]string{"--port=3001"})
		if port != "3001" {
			t.Errorf("--port= = %q, want 3001", port)
		}
	})

	t.Run("-port flag", func(t *testing.T) {
		port, _ := parseStartFlags([]string{"-port", "3002"})
		if port != "3002" {
			t.Errorf("-port = %q, want 3002", port)
		}
	})

	t.Run("-port= flag", func(t *testing.T) {
		port, _ := parseStartFlags([]string{"-port=3003"})
		if port != "3003" {
			t.Errorf("-port= = %q, want 3003", port)
		}
	})

	t.Run("--daemon flag", func(t *testing.T) {
		_, daemon := parseStartFlags([]string{"--daemon"})
		if !daemon {
			t.Error("--daemon = false, want true")
		}
	})

	t.Run("-d flag", func(t *testing.T) {
		_, daemon := parseStartFlags([]string{"-d"})
		if !daemon {
			t.Error("-d = false, want true")
		}
	})

	t.Run("mixed flags", func(t *testing.T) {
		port, daemon := parseStartFlags([]string{"-d", "--port", "4000"})
		if port != "4000" {
			t.Errorf("mixed port = %q, want 4000", port)
		}
		if !daemon {
			t.Error("mixed daemon = false, want true")
		}
	})
}
