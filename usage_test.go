package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderUsageSignal(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		u := accountUsage{Total: 0, Issues: 0, PRs: 0, Comments: 0}
		if got := renderUsageSignal(u, []string{"acme/web"}); got != "" {
			t.Errorf("renderUsageSignal(total=0) = %q, want empty", got)
		}
	})

	t.Run("no matching repos", func(t *testing.T) {
		u := accountUsage{Total: 3, Issues: 1, PRs: 1, Comments: 1, Repos: []string{"other/repo"}}
		got := renderUsageSignal(u, []string{"acme/web"})
		if !strings.Contains(got, "active repos you serve: (none)") {
			t.Errorf("renderUsageSignal no match = %q, want '(none)'", got)
		}
		if !strings.Contains(got, "1 issue · 1 PR · 1 comment events") {
			t.Errorf("renderUsageSignal no match missing counts: %q", got)
		}
	})

	t.Run("matching repos in order", func(t *testing.T) {
		u := accountUsage{Total: 5, Issues: 2, PRs: 2, Comments: 1, Repos: []string{"acme/api", "acme/web", "other/repo"}}
		got := renderUsageSignal(u, []string{"acme/web", "acme/api"})
		want := "acme/api, acme/web"
		if !strings.Contains(got, "active repos you serve: "+want) {
			t.Errorf("renderUsageSignal match = %q, want to contain %q", got, want)
		}
	})
}

func TestUsageContext(t *testing.T) {
	t.Run("not logged in", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		c := &Company{ghRepo: "acme/web"}
		if got := c.usageContext(); got != "" {
			t.Errorf("usageContext() with empty config = %q, want empty", got)
		}
	})

	t.Run("success", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" || r.URL.Path != "/api/usage" {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			auth := r.Header.Get("Authorization")
			if auth != "Bearer token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(accountUsage{
				Total:    3,
				Issues:   1,
				PRs:      1,
				Comments: 1,
				Repos:    []string{"acme/web"},
			})
		}))
		defer srv.Close()

		cfg := &cliConfig{PlatformURL: srv.URL, Token: "token"}
		if err := os.MkdirAll(filepath.Join(home, ".mago"), 0o755); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(cfg)
		if err := os.WriteFile(filepath.Join(home, ".mago", "config.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}

		c := &Company{ghRepo: "acme/web"}
		got := c.usageContext()
		for _, want := range []string{"1 issue", "1 PR", "1 comment", "acme/web"} {
			if !strings.Contains(got, want) {
				t.Errorf("usageContext() missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("error", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}))
		defer srv.Close()

		cfg := &cliConfig{PlatformURL: srv.URL, Token: "token"}
		if err := os.MkdirAll(filepath.Join(home, ".mago"), 0o755); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(cfg)
		if err := os.WriteFile(filepath.Join(home, ".mago", "config.json"), b, 0o600); err != nil {
			t.Fatal(err)
		}

		c := &Company{ghRepo: "acme/web"}
		if got := c.usageContext(); got != "" {
			t.Errorf("usageContext() on error = %q, want empty", got)
		}
	})
}
