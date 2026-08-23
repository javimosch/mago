package main

import (
	"os"
	"strings"
	"testing"
)

func TestGithubBackendAnswerHITL(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "comment" ]; then
		printf '%s' "$7" > "$0.body"
		exit 0
	elif [ "$3" = "issue" ] && [ "$4" = "edit" ]; then
		printf '%s\n' "$*" >> "$0.edits"
		exit 0
	fi
	exit 1`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	if err := b.AnswerHITL("7", "ship it"); err != nil {
		t.Fatalf("AnswerHITL: %v", err)
	}

	body, err := os.ReadFile(fake + "/gh.body")
	if err != nil {
		t.Fatalf("reading recorded body: %v", err)
	}
	got := string(body)
	if !strings.Contains(got, "CEO:") || !strings.Contains(got, "ship it") {
		t.Errorf("comment body missing expected text: %q", got)
	}

	edits, err := os.ReadFile(fake + "/gh.edits")
	if err != nil {
		t.Fatalf("reading recorded edits: %v", err)
	}
	s := string(edits)
	if !strings.Contains(s, labHITL) || !strings.Contains(s, labInProgress) {
		t.Errorf("edits missing expected labels: %q", s)
	}
}

func TestGithubBackendAnswerHITL_CommentError(t *testing.T) {
	fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "comment" ]; then
		echo "comment failed" >&2
		exit 1
	fi
	exit 1`)
	t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

	b := &githubBackend{repo: "acme/web"}
	err := b.AnswerHITL("7", "ship it")
	if err == nil {
		t.Fatal("expected error from AnswerHITL")
	}
	if !strings.Contains(err.Error(), "comment failed") {
		t.Errorf("error = %q, want to contain 'comment failed'", err.Error())
	}

	if _, statErr := os.Stat(fake + "/gh.edits"); statErr == nil {
		t.Error("expected no issue edit when comment fails")
	}
}
