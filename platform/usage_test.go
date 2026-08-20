package main

import (
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func TestUsageAggregation(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// account 1 is entitled to a/b and a/c
	st.db.Exec("INSERT INTO repo_grants (account_id, repo) VALUES (1, 'a/b'), (1, 'a/c')")
	if got := st.AccountForRepo("a/b"); got != 1 {
		t.Fatalf("AccountForRepo(a/b)=%d, want 1", got)
	}
	if got := st.AccountForRepo("x/y"); got != 0 {
		t.Errorf("unowned repo should resolve to 0, got %d", got)
	}
	// GitHub App path: account 2 owns an installation whose repos_json lists org/app-repo (the
	// real-user entitlement path — such repos never land in repo_grants).
	st.db.Exec("INSERT INTO installations (installation_id, account_id, repos_json, updated_at) VALUES (99, 2, '[\"org/app-repo\"]', 0)")
	if got := st.AccountForRepo("org/app-repo"); got != 2 {
		t.Errorf("App-installation repo should resolve to account 2, got %d", got)
	}

	for _, e := range []struct{ repo, event, action string }{
		{"a/b", "issues", "opened"},
		{"a/b", "issues", "labeled"},
		{"a/b", "pull_request", "opened"},
		{"a/b", "issue_comment", "created"},
		{"a/c", "issues", "opened"},
		{"a/b", "push", ""}, // "other"
	} {
		st.RecordGHEvent(1, e.repo, e.event, e.action)
	}

	u := st.UsageForAccount(1, 7)
	if u.Total != 6 || u.Issues != 3 || u.PRs != 1 || u.Comments != 1 || u.Other != 1 {
		t.Errorf("rollup wrong: %+v", u)
	}
	if len(u.Repos) != 2 {
		t.Errorf("active repos=%v, want 2", u.Repos)
	}
	if u.LastTs == 0 {
		t.Error("LastTs should be set")
	}

	all := st.UsageByAccount(7)
	if len(all) != 1 || all[0].AccountID != 1 || all[0].Total != 6 {
		t.Errorf("UsageByAccount=%+v", all)
	}

	// an account with no activity rolls up to zero, not a panic
	if z := st.UsageForAccount(999, 7); z.Total != 0 || len(z.Repos) != 0 {
		t.Errorf("empty account should be zero: %+v", z)
	}
}

func TestAgoStr(t *testing.T) {
	cases := []struct {
		name    string
		ts      int64
		pattern string
	}{
		{"never", 0, "^never$"},
		{"just now", time.Now().Unix(), "^just now$"},
		{"minutes", time.Now().Add(-30 * time.Minute).Unix(), `^\d+m ago$`},
		{"hours", time.Now().Add(-3 * time.Hour).Unix(), `^\d+h ago$`},
		{"days", time.Now().Add(-3 * 24 * time.Hour).Unix(), `^\d+d ago$`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := agoStr(c.ts)
			re := regexp.MustCompile(c.pattern)
			if !re.MatchString(got) {
				t.Errorf("agoStr(%d) = %q, want match %q", c.ts, got, c.pattern)
			}
		})
	}
}
