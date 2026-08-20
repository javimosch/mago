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
