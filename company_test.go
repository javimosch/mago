package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCompanyProjectConfsRoundTrip(t *testing.T) {
	c := newTestCompany(t)
	if err := c.saveProject("web", "acme/web", false); err != nil {
		t.Fatal(err)
	}
	if err := c.saveProject("mobile", "acme/mobile", true); err != nil {
		t.Fatal(err)
	}

	got := c.loadProjectConfs()
	want := map[string]projConf{
		"web":    {Repo: "acme/web", MirrorIssue: false},
		"mobile": {Repo: "acme/mobile", MirrorIssue: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("loadProjectConfs() = %v, want %v", got, want)
	}

	projects := c.loadProjects()
	wantProjects := map[string]string{"web": "acme/web", "mobile": "acme/mobile"}
	if !reflect.DeepEqual(projects, wantProjects) {
		t.Errorf("loadProjects() = %v, want %v", projects, wantProjects)
	}

	if got := c.projectRepo("web"); got != "acme/web" {
		t.Errorf("projectRepo(web) = %q, want %q", got, "acme/web")
	}
	if got := c.projectMirror("mobile"); !got {
		t.Error("projectMirror(mobile) = false, want true")
	}
}

func TestCompanyPathsAndRoles(t *testing.T) {
	c := newTestCompany(t)

	// Path helpers resolve under the company dir.
	if got := c.workspaceFor(nil); got != c.workspaceDir() {
		t.Errorf("workspaceFor(nil) = %q, want %q", got, c.workspaceDir())
	}
	if got := c.workspaceFor(&Task{Project: "web"}); got != c.projectDir("web") {
		t.Errorf("workspaceFor(Project=web) = %q, want %q", got, c.projectDir("web"))
	}
	if got := c.skillsIndex(); !strings.HasSuffix(got, ".mago/skills/INDEX.md") {
		t.Errorf("skillsIndex() = %q, want ending .mago/skills/INDEX.md", got)
	}

	// Role helpers.
	if !isPlannerRole(&Agent{Plans: true}) {
		t.Error("isPlannerRole should be true when Plans is true")
	}
	if isPlannerRole(&Agent{Plans: false}) {
		t.Error("isPlannerRole should be false when Plans is false")
	}

	if !isReviewerRole(&Agent{Reviews: true}) {
		t.Error("isReviewerRole should be true when Reviews is true")
	}
	if !isReviewerRole(&Agent{Name: "reviewer-1"}) {
		t.Error("isReviewerRole should match name containing 'review'")
	}
	if isReviewerRole(&Agent{Name: "coder", Title: "implementer"}) {
		t.Error("isReviewerRole should be false for non-review agent")
	}

	// Clarify instructions are non-empty and point at the mago:clarify / mago:go labels.
	if s := clarifyInstructions(); !strings.Contains(s, "mago:clarify") || !strings.Contains(s, "mago:go") {
		t.Errorf("clarifyInstructions missing expected labels: %q", s)
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	a := &Agent{Name: "coder", Title: "Implementer", Persona: "Writes clean, tested Go."}
	s := buildSystemPrompt(a)
	for _, want := range []string{
		"You are Implementer (coder)",
		"mago operating contract",
		"The files and the workspace are the source of truth",
		"Report ONLY what you actually did",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("buildSystemPrompt missing %q: %s", want, s)
		}
	}
}

func TestCompanyLoadMirrors(t *testing.T) {
	c := newTestCompany(t)
	if got := c.loadMirrors(); len(got) != 0 {
		t.Errorf("loadMirrors() with no file = %v, want empty", got)
	}

	if err := os.WriteFile(c.mirrorsFile(), []byte(`{"TASK-1": 42}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := c.loadMirrors()
	if got["TASK-1"] != 42 {
		t.Errorf("loadMirrors()[TASK-1] = %d, want 42", got["TASK-1"])
	}
}
