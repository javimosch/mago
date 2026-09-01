package main

import (
	"os"
	"strings"
	"testing"
)

// TestGithubBackendEnsureLabels verifies ensureLabels creates the five canonical mago labels.
func TestGithubBackendEnsureLabels(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "label" ] && [ "$4" = "create" ]; then
		printf '%s\n' "$5" >> "$0.labels"
		exit 0
	else
		exit 1
	fi`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	b.ensureLabels()

	got, err := os.ReadFile(fake + "/gh.labels")
	if err != nil {
		t.Fatalf("expected label create calls: %v", err)
	}
	want := []string{labInProgress, labBlocked, labHITL, labClarify, labGo}
	have := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(have) != len(want) {
		t.Fatalf("ensureLabels got %d label creates, want %d: %q", len(have), len(want), have)
	}
	for i, w := range want {
		if have[i] != w {
			t.Errorf("label[%d] = %q, want %q", i, have[i], w)
		}
	}
}
