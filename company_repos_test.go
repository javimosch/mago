package main

import (
	"os"
	"reflect"
	"testing"
)

// TestCompanyRepos_DedupsAndSorts verifies repos() returns the company backlog repo plus
// all configured project repos, deduplicated and sorted, and omits empty entries.
func TestCompanyRepos_DedupsAndSorts(t *testing.T) {
	t.Run("backlog repo plus projects", func(t *testing.T) {
		c := newTestCompany(t)
		c.ghRepo = "acme/backlog"
		if err := c.saveProject("p1", "acme/one", false); err != nil {
			t.Fatal(err)
		}
		if err := c.saveProject("p2", "acme/two", false); err != nil {
			t.Fatal(err)
		}

		got := c.repos()
		want := []string{"acme/backlog", "acme/one", "acme/two"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("repos() = %v, want %v", got, want)
		}
	})

	t.Run("deduplicates backlog that is also a project", func(t *testing.T) {
		c := newTestCompany(t)
		c.ghRepo = "acme/shared"
		if err := c.saveProject("p1", "acme/shared", false); err != nil {
			t.Fatal(err)
		}
		if err := c.saveProject("p2", "acme/other", false); err != nil {
			t.Fatal(err)
		}

		got := c.repos()
		want := []string{"acme/other", "acme/shared"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("repos() = %v, want %v", got, want)
		}
	})

	t.Run("projects only when no backlog repo", func(t *testing.T) {
		c := newTestCompany(t)
		c.ghRepo = ""
		if err := c.saveProject("b", "acme/b", false); err != nil {
			t.Fatal(err)
		}
		if err := c.saveProject("a", "acme/a", false); err != nil {
			t.Fatal(err)
		}

		got := c.repos()
		want := []string{"acme/a", "acme/b"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("repos() = %v, want %v", got, want)
		}
	})

	t.Run("empty when local-only and no projects", func(t *testing.T) {
		c := newTestCompany(t)
		c.ghRepo = ""
		os.Unsetenv("MAGO_GH_REPO")
		if got := c.repos(); len(got) != 0 {
			t.Errorf("repos() = %v, want empty", got)
		}
	})
}

// TestCompanyTaskRepo_ResolvesProjectRepo verifies taskRepo returns the project's repo
// when a task is scoped to a project, and falls back to the company backlog repo otherwise.
func TestCompanyTaskRepo_ResolvesProjectRepo(t *testing.T) {
	c := newTestCompany(t)
	c.ghRepo = "acme/backlog"
	if err := c.saveProject("frontend", "acme/web", false); err != nil {
		t.Fatal(err)
	}

	if got := c.taskRepo(&Task{Project: "frontend"}); got != "acme/web" {
		t.Errorf("taskRepo(project) = %q, want %q", got, "acme/web")
	}
	if got := c.taskRepo(&Task{Project: ""}); got != "acme/backlog" {
		t.Errorf("taskRepo(no project) = %q, want %q", got, "acme/backlog")
	}
	if got := c.taskRepo(&Task{Project: "unknown"}); got != "" {
		t.Errorf("taskRepo(unknown project) = %q, want empty", got)
	}
}

// TestEnsureMirrorIssue_CreatesAndCaches verifies that ensureMirrorIssue opens a GitHub
// issue via gh, persists the task→issue mapping, and returns the cached number on later
// calls (even if gh would fail) so resumes don't duplicate mirror issues.
func TestEnsureMirrorIssue_CreatesAndCaches(t *testing.T) {
	c := newTestCompany(t)
	task := &Task{ID: "task-1", Title: "Fix the thing", Body: "A detailed\nmultiline body."}
	repo := "acme/web"

	t.Run("create", func(t *testing.T) {
		fake := fakeGh(t, `if [ "$3" = "issue" ] && [ "$4" = "create" ]; then echo "https://github.com/acme/web/issues/7"; else exit 1; fi`)
		t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

		if got := c.ensureMirrorIssue(task, repo); got != 7 {
			t.Errorf("ensureMirrorIssue() = %d, want 7", got)
		}
		if m := c.loadMirrors(); m[task.ID] != 7 {
			t.Errorf("mirror map = %v, want task-1=7", m)
		}
	})

	t.Run("cached", func(t *testing.T) {
		// Even with a failing gh, the cached mapping should be returned.
		fake := fakeGh(t, `echo "should not run" >&2; exit 1`)
		t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

		if got := c.ensureMirrorIssue(task, repo); got != 7 {
			t.Errorf("ensureMirrorIssue(cached) = %d, want 7", got)
		}
	})

	t.Run("failure", func(t *testing.T) {
		c2 := newTestCompany(t)
		fake := fakeGh(t, `echo "no auth" >&2; exit 1`)
		t.Setenv("PATH", fake+":"+os.Getenv("PATH"))

		if got := c2.ensureMirrorIssue(&Task{ID: "task-2", Title: "Fail", Body: "x"}, repo); got != 0 {
			t.Errorf("ensureMirrorIssue(failure) = %d, want 0", got)
		}
	})
}
