package main

import (
	"strings"
	"testing"
)

// TestValidateGHRepo_AcceptsBareOwnerRepo verifies that well-formed `owner/repo` values
// (and the empty/unset value) pass validation.
func TestValidateGHRepo_AcceptsBareOwnerRepo(t *testing.T) {
	for _, repo := range []string{"", "acme/backlog", "javimosch/mago", "a/b", "Org123/repo-name.go"} {
		if err := validateGHRepo(repo); err != nil {
			t.Errorf("validateGHRepo(%q) = %v, want nil", repo, err)
		}
	}
}

// TestValidateGHRepo_RejectsURLs verifies that full GitHub URLs and git remotes are rejected
// with a message that names the expected form.
func TestValidateGHRepo_RejectsURLs(t *testing.T) {
	for _, repo := range []string{
		"https://github.com/acme/backlog",
		"http://github.com/acme/backlog",
		"git@github.com:acme/backlog.git",
		"github.com/acme/backlog",
		"ssh://git@github.com/acme/backlog",
	} {
		err := validateGHRepo(repo)
		if err == nil {
			t.Errorf("validateGHRepo(%q) = nil, want a URL error", repo)
			continue
		}
		assertNamesRepoForm(t, repo, err)
	}
}

// TestValidateGHRepo_RejectsMissingOwner verifies that values with no owner segment (or extra
// path segments) are rejected with an actionable message.
func TestValidateGHRepo_RejectsMissingOwner(t *testing.T) {
	for _, repo := range []string{
		"backlog",        // bare repo name, no owner
		"acme/",          // missing repo
		"/backlog",       // missing owner
		"acme/team/repo", // extra path segment
		"/",              // both empty
	} {
		err := validateGHRepo(repo)
		if err == nil {
			t.Errorf("validateGHRepo(%q) = nil, want a missing-owner error", repo)
			continue
		}
		assertNamesRepoForm(t, repo, err)
	}
}

// TestValidateGHRepo_AllowsRepoNameWithGithubDotCom verifies that a repo name which simply
// contains the substring "github.com" (e.g. acme/github.com-foo) is accepted; only values
// that actually start with github.com/ or look like URLs/remotes are rejected.
func TestValidateGHRepo_AllowsRepoNameWithGithubDotCom(t *testing.T) {
	for _, repo := range []string{"acme/github.com-foo", "acme/github.com_mirror", "my-org/libgithub.com"} {
		if err := validateGHRepo(repo); err != nil {
			t.Errorf("validateGHRepo(%q) = %v, want nil", repo, err)
		}
	}
}

// TestValidateGHRepo_RejectsGitSuffix verifies the common copy-from-clone-URL mistake of a
// trailing .git is caught.
func TestValidateGHRepo_RejectsGitSuffix(t *testing.T) {
	err := validateGHRepo("acme/backlog.git")
	if err == nil {
		t.Fatal("validateGHRepo(\"acme/backlog.git\") = nil, want a .git error")
	}
	assertNamesRepoForm(t, "acme/backlog.git", err)
}

// assertNamesRepoForm checks the validation error echoes the offending value and names the
// expected `owner/repo` form so the operator knows exactly what to fix.
func assertNamesRepoForm(t *testing.T, repo string, err error) {
	t.Helper()
	msg := err.Error()
	if !strings.Contains(msg, "MAGO_GH_REPO") {
		t.Errorf("error for %q should name MAGO_GH_REPO, got: %q", repo, msg)
	}
	if !strings.Contains(msg, "owner/repo") {
		t.Errorf("error for %q should name the owner/repo form, got: %q", repo, msg)
	}
}
