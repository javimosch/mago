package main

import (
	"os"
	"path/filepath"
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

func TestGithubToken(t *testing.T) {
	t.Run("env var wins", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("GITHUB_TOKEN", "ghp_env")
		t.Setenv("GITHUB_TOKEN_FILE", "")
		if got := githubToken(); got != "ghp_env" {
			t.Errorf("githubToken() = %q, want %q", got, "ghp_env")
		}
	})

	t.Run("padded env var is trimmed", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("GITHUB_TOKEN", "  ghp_padded  ")
		t.Setenv("GITHUB_TOKEN_FILE", "")
		if got := githubToken(); got != "ghp_padded" {
			t.Errorf("githubToken() = %q, want %q", got, "ghp_padded")
		}
	})

	t.Run("whitespace env falls back to file", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("GITHUB_TOKEN", "")
		path := filepath.Join(home, "token")
		if err := os.WriteFile(path, []byte("  ghp_file  "), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		t.Setenv("GITHUB_TOKEN_FILE", path)
		if got := githubToken(); got != "ghp_file" {
			t.Errorf("githubToken() = %q, want %q", got, "ghp_file")
		}
	})

	t.Run("default file path is expanded", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GITHUB_TOKEN_FILE", "")
		defaultPath := filepath.Join(home, ".github", "token")
		if err := os.MkdirAll(filepath.Dir(defaultPath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(defaultPath, []byte("ghp_default"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if got := githubToken(); got != "ghp_default" {
			t.Errorf("githubToken() = %q, want %q", got, "ghp_default")
		}
	})

	t.Run("missing env and file returns empty", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GITHUB_TOKEN_FILE", "")
		if got := githubToken(); got != "" {
			t.Errorf("githubToken() = %q, want empty", got)
		}
	})
}
