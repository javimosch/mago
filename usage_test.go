package main

import (
	"strings"
	"testing"
)

func TestRenderUsageSignal(t *testing.T) {
	// No activity -> empty signal (planner just follows the focus).
	if s := renderUsageSignal(accountUsage{Total: 0}, []string{"o/a"}); s != "" {
		t.Errorf("no activity should render empty, got %q", s)
	}

	// Activity is scoped to repos this company serves.
	u := accountUsage{
		Total: 9, Issues: 5, PRs: 2, Comments: 2,
		Repos: []string{"o/a", "o/other"}, // o/other is not served by this company
	}
	s := renderUsageSignal(u, []string{"o/a", "o/b"})
	for _, want := range []string{"5 issue", "2 PR", "2 comment", "o/a"} {
		if !strings.Contains(s, want) {
			t.Errorf("signal missing %q: %s", want, s)
		}
	}
	if strings.Contains(s, "o/other") {
		t.Errorf("signal should not include repos this company doesn't serve: %s", s)
	}

	// Activity exists on the account but none on this company's repos -> "(none)" active.
	s2 := renderUsageSignal(u, []string{"o/unrelated"})
	if !strings.Contains(s2, "(none)") {
		t.Errorf("no served-repo activity should show (none): %s", s2)
	}
}
