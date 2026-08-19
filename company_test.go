package main

import (
	"os"
	"reflect"
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
