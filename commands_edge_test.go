package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cmdTask surfaces a parseCompanyDir error (e.g. a bare -C with no directory)
// before it looks at the task args.
func TestCmdTask_ParseCompanyDirError(t *testing.T) {
	err := cmdTask([]string{"-C"})
	if err == nil {
		t.Fatal("expected error for bare -C")
	}
	if !strings.Contains(err.Error(), "-C needs a directory value") {
		t.Errorf("error = %q, want '-C needs a directory value'", err.Error())
	}
}

// cmdTask with no sub-action prints the usage error before loading the company.
func TestCmdTask_NoAction(t *testing.T) {
	dir := t.TempDir()

	if err := cmdTask([]string{"-C", dir}); err == nil {
		t.Fatal("expected usage error for missing action")
	}
}

// cmdTask surfaces a loadCompany error (e.g. a directory with no .mago/) instead
// of trying to add the task.
func TestCmdTask_LoadCompanyError(t *testing.T) {
	dir := t.TempDir() // no .mago/

	err := cmdTask([]string{"-C", dir, "add", "a title"})
	if err == nil {
		t.Fatal("expected error for missing .mago/")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error = %q, want 'not a mago company'", err.Error())
	}
}

// cmdProject surfaces a parseCompanyDir error (e.g. a bare -C) before flag parsing.
func TestCmdProject_ParseCompanyDirError(t *testing.T) {
	err := cmdProject([]string{"-C"})
	if err == nil {
		t.Fatal("expected error for bare -C")
	}
	if !strings.Contains(err.Error(), "-C needs a directory value") {
		t.Errorf("error = %q, want '-C needs a directory value'", err.Error())
	}
}

// cmdProject surfaces a loadCompany error (e.g. a directory with no .mago/)
// before dispatching the sub-action.
func TestCmdProject_LoadCompanyError(t *testing.T) {
	dir := t.TempDir() // no .mago/

	err := cmdProject([]string{"-C", dir, "list"})
	if err == nil {
		t.Fatal("expected error for missing .mago/")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error = %q, want 'not a mago company'", err.Error())
	}
}

// `mago project add --mirror` persists the mirror-issue flag and reports it on
// both the add output and `project list`.
func TestCmdProject_AddMirror(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	out := captureStdout(t, func() {
		if err := cmdProject([]string{"-C", dir, "add", "web", "--repo", "acme/web", "--mirror"}); err != nil {
			t.Fatalf("cmdProject add: %v", err)
		}
	})
	if !strings.Contains(out, `project "web" ready -> acme/web [mirror-issue on]`) {
		t.Errorf("unexpected add output: %q", out)
	}

	out = captureStdout(t, func() {
		if err := cmdProject([]string{"-C", dir, "list"}); err != nil {
			t.Fatalf("cmdProject list: %v", err)
		}
	})
	if !strings.Contains(out, "web -> acme/web  [mirror-issue]") {
		t.Errorf("expected mirror flag in list output: %q", out)
	}
}

// cmdStatus surfaces a loadCompany error (e.g. a directory with no .mago/)
// instead of printing an empty report.
func TestCmdStatus_LoadCompanyError(t *testing.T) {
	dir := t.TempDir() // no .mago/

	err := cmdStatus([]string{"-C", dir})
	if err == nil {
		t.Fatal("expected error for missing .mago/")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error = %q, want 'not a mago company'", err.Error())
	}
}

// cmdAnswer surfaces a parseCompanyDir error (e.g. a bare -C) before it tries
// to load the company.
func TestCmdAnswer_ParseCompanyDirError(t *testing.T) {
	err := cmdAnswer([]string{"-C"})
	if err == nil {
		t.Fatal("expected error for bare -C")
	}
	if !strings.Contains(err.Error(), "-C needs a directory value") {
		t.Errorf("error = %q, want '-C needs a directory value'", err.Error())
	}
}

// cmdInit returns the ensureDir error when the target path is blocked by an
// existing regular file, instead of scaffolding into it.
func TestCmdInit_EnsureDirError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdInit([]string{blocker}); err == nil {
		t.Fatal("expected error when target path is a regular file")
	}
}
