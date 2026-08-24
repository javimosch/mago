package main

import (
	"strings"
	"testing"
)

func TestHookBase(t *testing.T) {
	cases := []struct {
		name    string
		repo    string
		org     string
		want    string
		wantErr string
	}{
		{"repo path", "owner/repo", "", "repos/owner/repo", ""},
		{"org path", "", "acme", "orgs/acme", ""},
		{"repo without slash", "ownerrepo", "", "", "--repo must be owner/repo"},
		{"neither repo nor org", "", "", "", "need --repo owner/repo or --org <org>"},
		{"repo wins when both", "owner/repo", "acme", "repos/owner/repo", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := hookBase(c.repo, c.org)
			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("hookBase(%q, %q) = %q, want error", c.repo, c.org, got)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Errorf("hookBase(%q, %q) error = %q, want %q", c.repo, c.org, err.Error(), c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("hookBase(%q, %q): %v", c.repo, c.org, err)
			}
			if got != c.want {
				t.Errorf("hookBase(%q, %q) = %q, want %q", c.repo, c.org, got, c.want)
			}
		})
	}
}
