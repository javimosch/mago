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

func TestGhCount(t *testing.T) {
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	script := `#!/bin/sh
if [ "$1" = "-R" ] && [ "$2" = "acme/web" ] && [ "$3" = "pr" ] && [ "$6" = "open" ]; then
	echo "5"
	exit 0
fi
exit 1
`
	if err := os.WriteFile(ghPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if got := ghCount("acme/web", "pr", "list", "--state", "open", "--json", "number", "--jq", "length"); got != 5 {
		t.Errorf("ghCount success = %d, want 5", got)
	}
	if got := ghCount("acme/web", "pr", "list", "--state", "merged"); got != -1 {
		t.Errorf("ghCount failure = %d, want -1", got)
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

// TestCmdDigestLocalNoProjects exercises the other main branch of cmdDigest: a single,
// local-only company with no configured GitHub repo or projects, an unset/placeholder
// mission, and no daily budget cap. It verifies the digest still renders a readable
// summary without attempting any gh network calls.
func TestCmdDigestLocalNoProjects(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{".mago/agents", "tasks"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("setup: mkdir %s: %v", sub, err)
		}
	}
	state := `# testco — company state

## Mission
(Set by the CEO. Edit me.)

## Shipped
Nothing yet.

## In flight
Nothing yet.

## Decisions
None yet.

## Activity log
`
	if err := os.WriteFile(filepath.Join(dir, "STATE.md"), []byte(state), 0o644); err != nil {
		t.Fatalf("write STATE.md: %v", err)
	}

	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_DAILY_BUDGET", "")
	os.Unsetenv("MAGO_GH_REPO")
	os.Unsetenv("MAGO_DAILY_BUDGET")

	comp, err := loadCompany(dir)
	if err != nil {
		t.Fatalf("loadCompany: %v", err)
	}
	if comp.ghRepo != "" {
		t.Fatalf("expected no ghRepo, got %q", comp.ghRepo)
	}

	out := captureStdout(t, func() {
		if err := cmdDigest([]string{"-C", dir}); err != nil {
			t.Fatalf("cmdDigest: %v", err)
		}
	})

	if !strings.Contains(out, "Mission: (unset") {
		t.Errorf("digest should report unset mission, got:\n%s", out)
	}
	if !strings.Contains(out, "Backlog (local):") {
		t.Errorf("digest should show local backlog, got:\n%s", out)
	}
	if strings.Contains(out, "Pull requests") {
		t.Errorf("local-only digest should not query or print pull requests, got:\n%s", out)
	}
	if !strings.Contains(out, "no MAGO_DAILY_BUDGET cap set") {
		t.Errorf("digest should report no budget cap, got:\n%s", out)
	}
	if !strings.Contains(out, "Needs you (HITL): none") {
		t.Errorf("digest should report no HITL items, got:\n%s", out)
	}
}

// TestCmdDigestSingleProject exercises the single-project (MAGO_GH_REPO-only) branch of
// cmdDigest: when the company has one adopted backlog repo and no projects.json, the digest
// should print company-repo backlog counts and PR throughput without gh network calls.
func TestCmdDigestSingleProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".mago"), 0o755); err != nil {
		t.Fatalf("setup: mkdir .mago: %v", err)
	}

	// Fake gh that returns empty issue lists and a fixed PR count.
	ghDir := t.TempDir()
	script := `#!/bin/sh
if echo "$*" | grep -q issue; then
	echo "[]"
	exit 0
fi
if echo "$*" | grep -q pr; then
	echo "3"
	exit 0
fi
exit 1
`
	if err := os.WriteFile(filepath.Join(ghDir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", ghDir+":"+os.Getenv("PATH"))
	t.Setenv("MAGO_GH_REPO", "acme/repo")
	t.Setenv("MAGO_TASK_LABEL", "")

	out := captureStdout(t, func() {
		if err := cmdDigest([]string{"-C", dir}); err != nil {
			t.Fatalf("cmdDigest: %v", err)
		}
	})

	if !strings.Contains(out, "Backlog (acme/repo):") {
		t.Errorf("digest should label backlog with the company repo, got:\n%s", out)
	}
	if !strings.Contains(out, "Pull requests (last 24h):") {
		t.Errorf("digest should print PR throughput for a single project, got:\n%s", out)
	}
	if !strings.Contains(out, "shipped by mago") {
		t.Errorf("digest should include the mago-shipped metric, got:\n%s", out)
	}
}

// TestCmdDigest_MissionSet covers the branch where STATE.md has a non-placeholder
// ## Mission section, so cmdDigest prints it instead of the unset hint.
func TestCmdDigest_MissionSet(t *testing.T) {
	c := newTestCompany(t)
	state := `# testco — company state

## Mission
Ship the thing.

## Shipped
Nothing yet.

## In flight
Nothing yet.

## Decisions
None yet.

## Activity log
`
	if err := os.WriteFile(filepath.Join(c.Dir, "STATE.md"), []byte(state), 0o644); err != nil {
		t.Fatalf("write STATE.md: %v", err)
	}
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_DAILY_BUDGET", "")

	out := captureStdout(t, func() {
		if err := cmdDigest([]string{"-C", c.Dir}); err != nil {
			t.Fatalf("cmdDigest: %v", err)
		}
	})

	if !strings.Contains(out, "Mission: Ship the thing.") {
		t.Errorf("digest should print the set mission, got:\n%s", out)
	}
	if strings.Contains(out, "Mission: (unset") {
		t.Errorf("digest should not report an unset mission when one is set, got:\n%s", out)
	}
}

// TestCmdDigest_BadArgs verifies a bare -C flag is rejected with a clear usage error
// rather than being swallowed as a positional argument.
func TestCmdDigest_BadArgs(t *testing.T) {
	if err := cmdDigest([]string{"-C"}); err == nil {
		t.Fatal("cmdDigest with a bare -C should error")
	} else if !strings.Contains(err.Error(), "-C") {
		t.Errorf("error should mention the -C flag, got: %v", err)
	}
}

// TestCmdDigest_NotACompany verifies pointing -C at a directory without .mago/ fails
// with the "run mago init" hint instead of rendering an empty digest.
func TestCmdDigest_NotACompany(t *testing.T) {
	err := cmdDigest([]string{"-C", t.TempDir()})
	if err == nil {
		t.Fatal("cmdDigest on a non-company dir should error")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error should explain the dir is not a company, got: %v", err)
	}
}

// TestCmdDigest_BacklogListError verifies that when the task backend cannot list tasks
// (here, a failing gh), the digest degrades to a "could not list" note instead of
// aborting the whole report.
func TestCmdDigest_BacklogListError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".mago"), 0o755); err != nil {
		t.Fatalf("setup: mkdir .mago: %v", err)
	}

	ghDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(ghDir, "gh"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", ghDir+":"+os.Getenv("PATH"))
	t.Setenv("MAGO_GH_REPO", "acme/repo")
	t.Setenv("MAGO_TASK_LABEL", "")
	t.Setenv("MAGO_DAILY_BUDGET", "")

	out := captureStdout(t, func() {
		if err := cmdDigest([]string{"-C", dir}); err != nil {
			t.Fatalf("cmdDigest: %v", err)
		}
	})

	if !strings.Contains(out, "could not list tasks") {
		t.Errorf("digest should degrade on a listing failure, got:\n%s", out)
	}
	// ghCount failures render as "?" — the report still completes.
	if !strings.Contains(out, "merged ? · open ?") {
		t.Errorf("digest should render failed PR counts as ?, got:\n%s", out)
	}
}

// TestCmdDigest_HITLAndBudgetPause covers two human-facing sections at once: a pending
// HITL item in .mago/inbox must be listed under "Needs you", and an exhausted
// MAGO_DAILY_BUDGET must print the "paused" suffix so the CEO sees why work stopped.
func TestCmdDigest_HITLAndBudgetPause(t *testing.T) {
	dir := t.TempDir()
	inbox := filepath.Join(dir, ".mago", "inbox")
	for _, sub := range []string{".mago/agents", "tasks", ".mago/inbox"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("setup: mkdir %s: %v", sub, err)
		}
	}
	if err := os.WriteFile(filepath.Join(inbox, "task-7.md"),
		[]byte("from: dev\ntask: #7 ship it\n\nQUESTION:\nWhich env?"), 0o644); err != nil {
		t.Fatalf("write inbox item: %v", err)
	}
	// Today's usage already exceeds the cap -> overBudget() is true.
	usage := `{"day":"` + utcDay() + `","actions":2}`
	if err := os.WriteFile(filepath.Join(dir, ".mago", "usage.json"), []byte(usage), 0o644); err != nil {
		t.Fatalf("write usage.json: %v", err)
	}

	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_DAILY_BUDGET", "1")

	out := captureStdout(t, func() {
		if err := cmdDigest([]string{"-C", dir}); err != nil {
			t.Fatalf("cmdDigest: %v", err)
		}
	})

	if !strings.Contains(out, "Needs you (HITL):") || strings.Contains(out, "Needs you (HITL): none") {
		t.Errorf("digest should list the pending HITL item, got:\n%s", out)
	}
	if !strings.Contains(out, "Which env?") {
		t.Errorf("digest should include the HITL question, got:\n%s", out)
	}
	if !strings.Contains(out, "2 / 1 work cycles") {
		t.Errorf("digest should show budget usage 2 / 1, got:\n%s", out)
	}
	if !strings.Contains(out, "paused") {
		t.Errorf("digest should mark the company as budget-paused, got:\n%s", out)
	}
}

// TestCmdDigest_MultiProjectShippedByMago covers the multi-project branch when a company
// repo is ALSO configured: besides per-project counts, the digest must still print the
// company-repo "shipped by mago" autonomy metric.
func TestCmdDigest_MultiProjectShippedByMago(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{".mago/agents", "tasks"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("setup: mkdir %s: %v", sub, err)
		}
	}
	b, err := json.Marshal(map[string]string{"web": "acme/web"})
	if err != nil {
		t.Fatalf("marshal projects.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mago", "projects.json"), b, 0o644); err != nil {
		t.Fatalf("write projects.json: %v", err)
	}

	// Fake gh: issues -> empty list, pr counts -> fixed numbers.
	ghDir := t.TempDir()
	script := `#!/bin/sh
if echo "$*" | grep -q issue; then
	echo "[]"
	exit 0
fi
echo "4"
exit 0
`
	if err := os.WriteFile(filepath.Join(ghDir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}
	t.Setenv("PATH", ghDir+":"+os.Getenv("PATH"))
	t.Setenv("MAGO_GH_REPO", "acme/backlog")
	t.Setenv("MAGO_TASK_LABEL", "")
	t.Setenv("MAGO_DAILY_BUDGET", "")

	out := captureStdout(t, func() {
		if err := cmdDigest([]string{"-C", dir}); err != nil {
			t.Fatalf("cmdDigest: %v", err)
		}
	})

	if !strings.Contains(out, "web: merged 4 · open 4") {
		t.Errorf("digest should list the project PR counts, got:\n%s", out)
	}
	if !strings.Contains(out, "shipped by mago: 4 (on acme/backlog)") {
		t.Errorf("digest should include the company-repo mago-shipped metric, got:\n%s", out)
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
