package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestUserTrialActive(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()

	cases := []struct {
		plan      string
		trialEnds int64
		want      bool
	}{
		{"trial", future, true},
		{"trial", past, false},
		{"trial", 0, false},
		{"mago", future, false},
		{"free", future, false},
	}
	for _, c := range cases {
		u := &User{Plan: c.plan, TrialEnds: c.trialEnds}
		if got := u.trialActive(); got != c.want {
			t.Errorf("trialActive(plan=%q, trialEnds=%d) = %v, want %v", c.plan, c.trialEnds, got, c.want)
		}
	}
}

func TestUserEntitled(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()

	cases := []struct {
		plan      string
		trialEnds int64
		want      bool
	}{
		{"mago", 0, true},
		{"founding", 0, true},
		{"trial", future, true},
		{"trial", past, false},
		{"free", 0, false},
		{"free", future, false},
	}
	for _, c := range cases {
		u := &User{Plan: c.plan, TrialEnds: c.trialEnds}
		if got := u.entitled(); got != c.want {
			t.Errorf("entitled(plan=%q, trialEnds=%d) = %v, want %v", c.plan, c.trialEnds, got, c.want)
		}
	}
}

func TestReposToJSON(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"b", "a"}, `["a","b"]`},
		{[]string{}, "[]"},
		{[]string{"repo"}, `["repo"]`},
	}
	for _, c := range cases {
		got := reposToJSON(c.in)
		if got != c.want {
			t.Errorf("reposToJSON(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStoreLookups(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	u, err := st.Create("dev@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	byEmail := st.GetByEmail("dev@example.com")
	if byEmail == nil || byEmail.ID != u.ID {
		t.Errorf("GetByEmail returned %+v, want user %d", byEmail, u.ID)
	}

	if got := st.GetByEmail("missing@example.com"); got != nil {
		t.Errorf("GetByEmail(missing) = %+v, want nil", got)
	}

	if err := st.Update(u.ID, func(u *User) { u.LicenseKey = "lic-test-123" }); err != nil {
		t.Fatalf("Update license: %v", err)
	}

	byLicense := st.GetByLicense("lic-test-123")
	if byLicense == nil || byLicense.ID != u.ID {
		t.Errorf("GetByLicense returned %+v, want user %d", byLicense, u.ID)
	}

	if got := st.GetByLicense(""); got != nil {
		t.Errorf("GetByLicense(\"\") = %+v, want nil", got)
	}
	if got := st.GetByLicense("no-such"); got != nil {
		t.Errorf("GetByLicense(no-such) = %+v, want nil", got)
	}
}

func TestFirstEvent(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if !st.FirstEvent("evt-1") {
		t.Errorf("FirstEvent(evt-1) first insert = false, want true")
	}
	if st.FirstEvent("evt-1") {
		t.Errorf("FirstEvent(evt-1) second insert = true, want false")
	}
	if !st.FirstEvent("evt-2") {
		t.Errorf("FirstEvent(evt-2) = false, want true")
	}
	if st.FirstEvent("") {
		t.Errorf("FirstEvent(\"\") = true, want false")
	}
}

func TestRepoGrants(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	u, err := st.Create("dev@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := st.EntitledRepos(u.ID); len(got) != 0 {
		t.Errorf("EntitledRepos before grants = %v, want empty", got)
	}

	if err := st.GrantRepo(u.ID, "owner/repo-a"); err != nil {
		t.Fatalf("GrantRepo: %v", err)
	}
	if err := st.GrantRepo(u.ID, "owner/repo-b"); err != nil {
		t.Fatalf("GrantRepo: %v", err)
	}

	got := st.EntitledRepos(u.ID)
	if !got["owner/repo-a"] || !got["owner/repo-b"] || len(got) != 2 {
		t.Errorf("EntitledRepos after grants = %v, want [owner/repo-a owner/repo-b]", got)
	}

	if err := st.RevokeRepo(u.ID, "owner/repo-a"); err != nil {
		t.Fatalf("RevokeRepo: %v", err)
	}
	got = st.EntitledRepos(u.ID)
	if got["owner/repo-a"] {
		t.Errorf("EntitledRepos after revoke still contains owner/repo-a")
	}
	if !got["owner/repo-b"] {
		t.Errorf("EntitledRepos after revoke missing owner/repo-b")
	}

	if err := st.RevokeRepo(u.ID, "not-granted"); err != nil {
		t.Errorf("RevokeRepo on missing repo = %v, want nil", err)
	}
}

func TestInstallations(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	u, err := st.Create("dev@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := st.UpsertInstallation(1, "org-a", []string{"repo2", "repo1"}); err != nil {
		t.Fatalf("UpsertInstallation: %v", err)
	}

	have := st.installRepos(1)
	want := map[string]bool{"repo1": true, "repo2": true}
	if len(have) != 2 || !want[have[0]] || !want[have[1]] {
		t.Errorf("installRepos after upsert = %v, want repo1+repo2", have)
	}

	if err := st.MutateInstallationRepos(1, "org-a", []string{"repo3"}, []string{"repo1"}); err != nil {
		t.Fatalf("MutateInstallationRepos: %v", err)
	}
	have = st.installRepos(1)
	want = map[string]bool{"repo2": true, "repo3": true}
	if len(have) != 2 || !want[have[0]] || !want[have[1]] {
		t.Errorf("installRepos after mutate = %v, want repo2+repo3", have)
	}

	if err := st.ClaimInstallation(1, u.ID); err != nil {
		t.Fatalf("ClaimInstallation: %v", err)
	}

	ins := st.InstallationsForAccount(u.ID)
	if len(ins) != 1 || ins[0].ID != 1 || ins[0].GithubLogin != "org-a" {
		t.Errorf("InstallationsForAccount = %+v, want one org-a installation", ins)
	}

	if err := st.DeleteInstallation(1); err != nil {
		t.Fatalf("DeleteInstallation: %v", err)
	}
	if got := st.installRepos(1); len(got) != 0 {
		t.Errorf("installRepos after delete = %v, want empty", got)
	}

	if err := st.ClaimInstallation(99, u.ID); err == nil {
		t.Errorf("ClaimInstallation on missing id = nil, want error")
	}
}

// TestEntitledRepos_Union verifies that EntitledRepos combines repos from both
// claimed GitHub App installations (repos_json) and direct repo_grants.
func TestEntitledRepos_Union(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "entitled.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	u, err := st.Create("dev@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := st.UpsertInstallation(1, "org-a", []string{"org-a/one", "org-a/two"}); err != nil {
		t.Fatalf("UpsertInstallation: %v", err)
	}
	if err := st.ClaimInstallation(1, u.ID); err != nil {
		t.Fatalf("ClaimInstallation: %v", err)
	}

	got := st.EntitledRepos(u.ID)
	if !got["org-a/one"] || !got["org-a/two"] || len(got) != 2 {
		t.Errorf("EntitledRepos after claim = %v, want org-a/one and org-a/two", got)
	}

	if err := st.GrantRepo(u.ID, "direct/three"); err != nil {
		t.Fatalf("GrantRepo: %v", err)
	}

	got = st.EntitledRepos(u.ID)
	if !got["org-a/one"] || !got["org-a/two"] || !got["direct/three"] || len(got) != 3 {
		t.Errorf("EntitledRepos union = %v, want three repos", got)
	}
}

func TestRecentEvents(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "recent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	u, err := st.Create("dev@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	st.LogEvent("signup", u.ID, "first")
	st.LogEvent("worker_connect", u.ID, "second")

	events := st.RecentEvents(10)
	if len(events) != 2 {
		t.Fatalf("RecentEvents(10) = %d events, want 2", len(events))
	}
	if events[0].Kind != "worker_connect" || events[0].Detail != "second" || events[0].Email != u.Email {
		t.Errorf("most recent event = %+v, want worker_connect/second/%s", events[0], u.Email)
	}
	if events[1].Kind != "signup" || events[1].Detail != "first" || events[1].Email != u.Email {
		t.Errorf("second event = %+v, want signup/first/%s", events[1], u.Email)
	}

	if got := len(st.RecentEvents(1)); got != 1 {
		t.Errorf("RecentEvents(1) = %d, want 1", got)
	}

	if got := len(st.RecentEvents(0)); got != 2 {
		t.Errorf("RecentEvents(0) default limit = %d, want 2", got)
	}
}
