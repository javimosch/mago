package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateGHRepo(t *testing.T) {
	cases := []struct {
		repo    string
		wantErr string
	}{
		{repo: "", wantErr: ""},
		{repo: "acme/backlog", wantErr: ""},
		{repo: "https://github.com/acme/backlog", wantErr: "looks like a URL or git remote"},
		{repo: "git@github.com:acme/backlog.git", wantErr: "looks like a URL or git remote"},
		{repo: "github.com/acme/backlog", wantErr: "looks like a URL or git remote"},
		{repo: "backlog", wantErr: "missing an owner or repo"},
		{repo: "/backlog", wantErr: "missing an owner or repo"},
		{repo: "acme/", wantErr: "missing an owner or repo"},
		{repo: "acme/backlog/extra", wantErr: "missing an owner or repo"},
		{repo: "acme/backlog.git", wantErr: "trailing .git"},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("repo=%q", tc.repo), func(t *testing.T) {
			err := validateGHRepo(tc.repo)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateGHRepo(%q) = %v, want nil", tc.repo, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateGHRepo(%q) = nil, want error containing %q", tc.repo, tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateGHRepo(%q) error = %q, want containing %q", tc.repo, err.Error(), tc.wantErr)
			}
		})
	}
}
