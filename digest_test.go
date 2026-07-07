package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSortedProjectNames verifies the digest's project ordering is alphabetical and
// case-sensitive-stable regardless of map iteration order, so multi-project digest output
// (PR grouping by project) is deterministic across runs.
func TestSortedProjectNames(t *testing.T) {
	cases := []struct {
		name     string
		projects map[string]projConf
		want     []string
	}{
		{"empty", map[string]projConf{}, nil},
		{"single", map[string]projConf{"solo": {Repo: "acme/solo"}}, []string{"solo"}},
		{
			"multiple sorted alphabetically",
			map[string]projConf{
				"zeta":  {Repo: "acme/zeta"},
				"alpha": {Repo: "acme/alpha"},
				"mid":   {Repo: "acme/mid"},
			},
			[]string{"alpha", "mid", "zeta"},
		},
		{
			"mixed case sorts by byte order",
			map[string]projConf{
				"Beta":  {Repo: "acme/beta"},
				"alpha": {Repo: "acme/alpha"},
			},
			[]string{"Beta", "alpha"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sortedProjectNames(tc.projects)
			if len(got) != len(tc.want) {
				t.Fatalf("sortedProjectNames() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("sortedProjectNames()[%d] = %q, want %q (full: %v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

func TestNumOr(t *testing.T) {
	if got := numOr(-1); got != "?" {
		t.Errorf("numOr(-1) = %q, want %q", got, "?")
	}
	if got := numOr(0); got != "0" {
		t.Errorf("numOr(0) = %q, want %q", got, "0")
	}
	if got := numOr(42); got != "42" {
		t.Errorf("numOr(42) = %q, want %q", got, "42")
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("one\ntwo\nthree"); got != "one" {
		t.Errorf("firstLine() = %q, want %q", got, "one")
	}
	if got := firstLine("solo line"); got != "solo line" {
		t.Errorf("firstLine() = %q, want %q", got, "solo line")
	}
	if got := firstLine(""); got != "" {
		t.Errorf("firstLine(\"\") = %q, want empty", got)
	}
}

// TestCmdDigestGroupsPRsByProject exercises cmdDigest end-to-end against a multi-project
// company (task #75): with two distinct, non-adopted project repos configured, the "Pull
// requests" section must list each project by name, in the same alphabetical order as
// sortedProjectNames, rather than collapsing to a single company-repo line. The configured
// repos don't exist on GitHub, so the underlying gh queries fail fast (verified separately)
// and ghCount degrades to -1 ("?") without hitting the network in a way that blocks the test.
func TestCmdDigestGroupsPRsByProject(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{".mago/agents", "tasks"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("setup: mkdir %s: %v", sub, err)
		}
	}
	projects := map[string]string{
		"zeta":  "mago-test-org/zeta-does-not-exist",
		"alpha": "mago-test-org/alpha-does-not-exist",
	}
	b, err := json.Marshal(projects)
	if err != nil {
		t.Fatalf("marshal projects.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mago", "projects.json"), b, 0o644); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}

	os.Unsetenv("MAGO_GH_REPO") // two distinct project repos -> ambiguous, no auto-adopted backlog repo
	comp, err := loadCompany(dir)
	if err != nil {
		t.Fatalf("loadCompany: %v", err)
	}
	if comp.ghRepo != "" {
		t.Fatalf("expected no auto-adopted backlog repo with 2 distinct project repos, got %q", comp.ghRepo)
	}

	out := captureStdout(t, func() {
		if err := cmdDigest([]string{"-C", dir}); err != nil {
			t.Fatalf("cmdDigest: %v", err)
		}
	})

	prSection := out
	if i := strings.Index(out, "Pull requests"); i >= 0 {
		prSection = out[i:]
	} else {
		t.Fatalf("digest output missing \"Pull requests\" section:\n%s", out)
	}

	alphaIdx := strings.Index(prSection, "alpha:")
	zetaIdx := strings.Index(prSection, "zeta:")
	if alphaIdx < 0 || zetaIdx < 0 {
		t.Fatalf("digest output should list both projects by name, got:\n%s", prSection)
	}
	if alphaIdx > zetaIdx {
		t.Errorf("expected alpha before zeta (alphabetical grouping), got:\n%s", prSection)
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	w.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(b)
}
