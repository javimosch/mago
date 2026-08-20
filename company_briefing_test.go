package main

import (
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
