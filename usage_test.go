package main

import (
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
