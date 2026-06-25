package main

import (
	"path/filepath"
	"testing"
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
