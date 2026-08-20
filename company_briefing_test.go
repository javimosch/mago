package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectRepoInstructions verifies role-specific git/gh instructions
// and the mirror-issue PR body placeholder.
func TestProjectRepoInstructions(t *testing.T) {
	reviewer := &Agent{Reviews: true}
	impl := &Agent{Name: "coder", Title: "Implementer"}
	repo := "acme/web"
	taskID := "42"

	rev := projectRepoInstructions(reviewer, repo, taskID, 0)
	for _, want := range []string{"You are REVIEWING", "Do NOT write or modify code", "gh pr list --head mago/"} {
		if !strings.Contains(rev, want) {
			t.Errorf("reviewer instructions missing %q:\n%s", want, rev)
		}
	}

	imp := projectRepoInstructions(impl, repo, taskID, 0)
	for _, want := range []string{"You are IMPLEMENTING", "mago/task-42", "mago task #42"} {
		if !strings.Contains(imp, want) {
			t.Errorf("implementer instructions missing %q:\n%s", want, imp)
		}
	}

	mirrored := projectRepoInstructions(impl, repo, taskID, 7)
	if !strings.Contains(mirrored, "Closes #7") {
		t.Errorf("implementer instructions with mirror missing 'Closes #7':\n%s", mirrored)
	}
}

// TestRecentJournalSummaries verifies the empty state, reverse chronological
// ordering, and N-limit behavior of the per-agent run summary helper.
func TestRecentJournalSummaries(t *testing.T) {
	c := newTestCompany(t)
	agent := "coder"
	runsDir := filepath.Join(c.runsDir(), agent)

	if got := c.recentJournalSummaries(agent, 3); got != "(none yet)" {
		t.Errorf("missing agent dir: got %q, want (none yet)", got)
	}

	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := c.recentJournalSummaries(agent, 3); got != "(none yet)" {
		t.Errorf("empty agent dir: got %q, want (none yet)", got)
	}

	for _, f := range []string{"2023-10-01T10-00-00.json", "2023-10-02T11-00-00.json", "2023-10-03T12-00-00.json"} {
		path := filepath.Join(runsDir, f)
		summary := strings.TrimSuffix(f, ".json")
		content := fmt.Sprintf(`{"ts":%q,"summary":"run %s"}`, summary, summary)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := c.recentJournalSummaries(agent, 2)
	for _, want := range []string{"2023-10-03T12-00-00", "2023-10-02T11-00-00"} {
		if !strings.Contains(got, want) {
			t.Errorf("latest two runs missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "2023-10-01T10-00-00") {
		t.Errorf("oldest run should be excluded by limit:\n%s", got)
	}
}

// TestOpenPRsText verifies parsing of gh pr list output and graceful fallback
// when the gh call fails or returns malformed JSON.
func TestOpenPRsText(t *testing.T) {
	c := newTestCompany(t)
	repo := "acme/web"

	t.Run("success", func(t *testing.T) {
		fake := fakeGh(t, `if [ "$3" = "pr" ] && [ "$4" = "list" ]; then echo '[{"number":3,"title":"fix thing","headRefName":"mago/task-123"}]'; else echo '[]'; fi`)
		t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
		got := c.openPRsText(repo)
		if !strings.Contains(got, "#3") || !strings.Contains(got, "mago/task-123") || !strings.Contains(got, "fix thing") {
			t.Errorf("openPRsText missing expected PR:\n%s", got)
		}
	})

	t.Run("gh failure", func(t *testing.T) {
		fake := fakeGh(t, `echo "no auth" >&2; exit 1`)
		t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
		got := c.openPRsText(repo)
		if got != "(could not list)" {
			t.Errorf("gh failure: got %q, want (could not list)", got)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		fake := fakeGh(t, `echo "not json"`)
		t.Setenv("PATH", fake+":"+os.Getenv("PATH"))
		got := c.openPRsText(repo)
		if got != "(none open)" {
			t.Errorf("malformed json: got %q, want (none open)", got)
		}
	})
}

// fakeGh creates a temp directory containing an executable `gh` shell stub
// with the supplied body and returns that directory.
func fakeGh(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" + body + "\n"
	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}
