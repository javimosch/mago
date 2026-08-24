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

// TestValidateProjectsConfig_Malformed verifies a corrupted projects.json is
// rejected with an actionable error that names the file and suggests recovery.
func TestValidateProjectsConfig_Malformed(t *testing.T) {
	c := newTestCompany(t)
	if err := os.WriteFile(c.projectsConfigFile(), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write malformed projects.json: %v", err)
	}
	err := c.validateProjectsConfig()
	if err == nil {
		t.Fatal("expected error for malformed projects.json")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error = %q, want 'not valid JSON'", err.Error())
	}
}

// TestValidateGHRepo verifies the MAGO_GH_REPO sanity checks: valid bare owner/repo,
// empty (unset), common URL/remote mistakes, missing owner or repo, and trailing .git.
func TestValidateGHRepo(t *testing.T) {
	tests := []struct {
		repo string
		want string // expected substring in error; empty means no error
	}{
		{"", ""},
		{"acme/backlog", ""},
		{"https://github.com/acme/backlog", "looks like a URL or git remote"},
		{"git@github.com:acme/backlog.git", "looks like a URL or git remote"},
		{"github.com/acme/backlog", "looks like a URL or git remote"},
		{"backlog", "missing an owner or repo"},
		{"/acme/backlog", "missing an owner or repo"},
		{"acme/", "missing an owner or repo"},
		{"acme/backlog/extra", "missing an owner or repo"},
		{"acme/backlog.git", "trailing .git"},
	}
	for _, tc := range tests {
		err := validateGHRepo(tc.repo)
		if tc.want == "" {
			if err != nil {
				t.Errorf("validateGHRepo(%q) = %v, want nil", tc.repo, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("validateGHRepo(%q) = nil, want error containing %q", tc.repo, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("validateGHRepo(%q) = %q, want containing %q", tc.repo, err.Error(), tc.want)
		}
	}
}
