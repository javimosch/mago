package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseCompanyDir_Defaults verifies the default company dir is the cwd ("." ) when
// neither -C nor $MAGO_COMPANY is given, and that positional args pass through untouched.
func TestParseCompanyDir_Defaults(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	dir, rest, err := parseCompanyDir([]string{"add", "a title"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "." {
		t.Errorf("dir = %q, want %q", dir, ".")
	}
	if strings.Join(rest, " ") != "add a title" {
		t.Errorf("rest = %v, want [add a title]", rest)
	}
}

// TestParseCompanyDir_FlagSetsDirAndStripsIt verifies -C <dir> sets the dir and removes both
// tokens from the returned positional args, regardless of position in the arg list.
func TestParseCompanyDir_FlagSetsDirAndStripsIt(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	dir, rest, err := parseCompanyDir([]string{"add", "title", "-C", "/srv/acme"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/srv/acme" {
		t.Errorf("dir = %q, want /srv/acme", dir)
	}
	if strings.Join(rest, " ") != "add title" {
		t.Errorf("rest = %v, want [add title]", rest)
	}
}

// TestParseCompanyDir_EnvDefaultOverriddenByFlag verifies $MAGO_COMPANY supplies the default
// dir but an explicit -C wins.
func TestParseCompanyDir_EnvDefaultOverriddenByFlag(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "/env/dir")

	dir, _, err := parseCompanyDir([]string{"status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/env/dir" {
		t.Errorf("dir = %q, want /env/dir (from env)", dir)
	}

	dir, _, err = parseCompanyDir([]string{"status", "-C", "/flag/dir"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/flag/dir" {
		t.Errorf("dir = %q, want /flag/dir (flag overrides env)", dir)
	}
}

// TestParseCompanyDir_BareFlagRejected verifies a -C with no following directory value (or an
// empty/whitespace value) is rejected instead of being silently swallowed as a positional arg.
func TestParseCompanyDir_BareFlagRejected(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	cases := [][]string{
		{"-C"},                // trailing bare flag
		{"status", "-C"},      // bare flag after a positional
		{"-C", ""},            // empty value
		{"-C", "   "},         // whitespace-only value
		{"task", "add", "-C"}, // bare flag at the end of a real command
	}
	for _, args := range cases {
		dir, rest, err := parseCompanyDir(args)
		if err == nil {
			t.Errorf("parseCompanyDir(%v): expected error, got dir=%q rest=%v", args, dir, rest)
			continue
		}
		if !strings.Contains(err.Error(), "-C") {
			t.Errorf("parseCompanyDir(%v): error %q should mention -C", args, err)
		}
	}
}

// TestLoadCompany_MissingCompanyNamesRemedies verifies the not-a-mago-company error points the
// user at both remedies: the -C <dir> flag and the $MAGO_COMPANY env var.
func TestLoadCompany_MissingCompanyNamesRemedies(t *testing.T) {
	dir := t.TempDir() // empty dir with no .mago/
	_, err := loadCompany(dir)
	if err == nil {
		t.Fatal("expected error for a directory without .mago/, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "-C") {
		t.Errorf("error %q should name the -C <dir> remedy", msg)
	}
	if !strings.Contains(msg, "MAGO_COMPANY") {
		t.Errorf("error %q should name the $MAGO_COMPANY remedy", msg)
	}
}

// TestWriteIfMissing_CreatesOnlyWhenAbsent verifies writeIfMissing writes the default
// content when the file is missing and leaves an existing file untouched.
func TestWriteIfMissing_CreatesOnlyWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/starter.txt"

	writeIfMissing(path, "default")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}
	if string(b) != "default" {
		t.Errorf("created content = %q, want %q", string(b), "default")
	}

	writeIfMissing(path, "changed")
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file to remain readable: %v", err)
	}
	if string(b) != "default" {
		t.Errorf("existing content was overwritten to %q, want %q", string(b), "default")
	}
}

// TestBackfillAgentFlag_AddsMissingFlagAndNoOps verifies backfillAgentFlag injects a
// missing frontmatter flag, preserves an already-set flag, and is a no-op on missing files.
func TestBackfillAgentFlag_AddsMissingFlagAndNoOps(t *testing.T) {
	dir := t.TempDir()

	// Missing file is a no-op.
	backfillAgentFlag(dir+"/missing.md", "plans", "true")

	path := dir + "/agent.md"
	os.WriteFile(path, []byte("---\nname: tester\n---\nbody\n"), 0o644)
	backfillAgentFlag(path, "plans", "true")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "plans: true") {
		t.Errorf("updated frontmatter missing plans flag:\n%s", got)
	}

	// Already-set value should be left unchanged.
	backfillAgentFlag(path, "plans", "true")
	b2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to re-read file: %v", err)
	}
	if string(b2) != got {
		t.Errorf("re-running backfill changed the file unexpectedly")
	}
}

// TestCmdInit_ScaffoldsCompany verifies cmdInit creates the expected directory layout,
// seeds the starter team, and writes the company state, vision and roadmap files.
func TestCmdInit_ScaffoldsCompany(t *testing.T) {
	dir := t.TempDir() + "/acme"
	if err := cmdInit([]string{dir}); err != nil {
		t.Fatalf("cmdInit: %v", err)
	}

	for _, sub := range []string{
		".mago/agents", ".mago/skills", ".mago/runs", ".mago/inbox",
		"tasks", "workspace", "projects",
	} {
		if _, err := os.Stat(filepath.Join(dir, sub)); err != nil {
			t.Errorf("missing directory %q: %v", sub, err)
		}
	}

	for _, f := range []string{"STATE.md", "VISION.md", "ROADMAP.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing file %q: %v", f, err)
		}
	}

	state, err := os.ReadFile(filepath.Join(dir, "STATE.md"))
	if err != nil {
		t.Fatalf("failed to read STATE.md: %v", err)
	}
	if !strings.Contains(string(state), "acme — company state") {
		t.Errorf("STATE.md does not contain expected company name; got:\n%s", state)
	}
}

func TestIfStr(t *testing.T) {
	cases := []struct {
		cond     bool
		trueVal  string
		falseVal string
		want     string
	}{
		{true, "yes", "no", "yes"},
		{false, "yes", "no", "no"},
		{true, "", "fallback", ""},
		{false, "ignored", "", ""},
	}
	for _, tc := range cases {
		if got := ifStr(tc.cond, tc.trueVal, tc.falseVal); got != tc.want {
			t.Errorf("ifStr(%v, %q, %q) = %q, want %q", tc.cond, tc.trueVal, tc.falseVal, got, tc.want)
		}
	}
}

func TestCmdTask_AddCreatesTask(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	out := captureStdout(t, func() {
		if err := cmdTask([]string{"-C", dir, "add", "cover", "cmdTask"}); err != nil {
			t.Fatalf("cmdTask: %v", err)
		}
	})

	if !strings.Contains(out, "created task #1: cover cmdTask") {
		t.Errorf("unexpected output: %q", out)
	}

	if _, err := os.Stat(filepath.Join(dir, "tasks", "task-1.md")); err != nil {
		t.Errorf("task file not created: %v", err)
	}
}

func TestCmdTask_AddWithProject(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	out := captureStdout(t, func() {
		if err := cmdTask([]string{"-C", dir, "add", "ship", "web", "--project", "web"}); err != nil {
			t.Fatalf("cmdTask: %v", err)
		}
	})

	if !strings.Contains(out, "created task #1: ship web [project: web]") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestCmdTask_MissingTitle(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	if err := cmdTask([]string{"-C", dir, "add"}); err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestCmdProject_ListEmpty(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	out := captureStdout(t, func() {
		if err := cmdProject([]string{"-C", dir, "list"}); err != nil {
			t.Fatalf("cmdProject: %v", err)
		}
	})

	if !strings.Contains(out, "no projects") {
		t.Errorf("expected empty project message, got: %q", out)
	}
}

func TestCmdProject_AddAndList(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	out := captureStdout(t, func() {
		if err := cmdProject([]string{"-C", dir, "add", "web", "--repo", "acme/web"}); err != nil {
			t.Fatalf("cmdProject add: %v", err)
		}
	})
	if !strings.Contains(out, `project "web" ready -> acme/web`) {
		t.Errorf("unexpected add output: %q", out)
	}

	if _, err := os.Stat(filepath.Join(dir, "projects", "web")); err != nil {
		t.Errorf("project directory not created: %v", err)
	}

	out = captureStdout(t, func() {
		if err := cmdProject([]string{"-C", dir, "list"}); err != nil {
			t.Fatalf("cmdProject list: %v", err)
		}
	})
	if !strings.Contains(out, "web -> acme/web") {
		t.Errorf("expected project in list, got: %q", out)
	}
}

func TestCmdProject_AddShorthandOwnerRepo(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	out := captureStdout(t, func() {
		if err := cmdProject([]string{"-C", dir, "add", "acme/web"}); err != nil {
			t.Fatalf("cmdProject: %v", err)
		}
	})
	if !strings.Contains(out, `project "web" ready -> acme/web`) {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestCmdProject_UnknownActionSuggests(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	err := cmdProject([]string{"-C", dir, "ad"})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("expected suggestion, got: %v", err)
	}
}

func TestCmdProject_MissingArgs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	if err := cmdProject([]string{"-C", dir}); err == nil {
		t.Fatal("expected usage error")
	}
}

func TestCmdStatus_Empty(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	out := captureStdout(t, func() {
		if err := cmdStatus([]string{"-C", dir}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})

	if !strings.Contains(out, "# company:") {
		t.Errorf("expected company header, got: %q", out)
	}
	if !strings.Contains(out, "(no STATE.md)") {
		t.Errorf("expected missing state marker, got: %q", out)
	}
	if !strings.Contains(out, "(none)") {
		t.Errorf("expected empty task list, got: %q", out)
	}
}

func TestCmdStatus_WithTask(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	if err := cmdTask([]string{"-C", dir, "add", "cover status"}); err != nil {
		t.Fatalf("cmdTask: %v", err)
	}

	out := captureStdout(t, func() {
		if err := cmdStatus([]string{"-C", dir}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})

	if !strings.Contains(out, "#1 [open] cover status") {
		t.Errorf("expected task in status output, got: %q", out)
	}
}

// TestCmdAnswer_RecordsResponse verifies the `mago answer` command appends the
// human response to an existing task and flips its status to in_progress.
func TestCmdAnswer_RecordsResponse(t *testing.T) {
	c := newTestCompany(t)
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")

	if err := cmdTask([]string{"-C", c.Dir, "add", "answer", "this"}); err != nil {
		t.Fatalf("cmdTask: %v", err)
	}

	out := captureStdout(t, func() {
		if err := cmdAnswer([]string{"-C", c.Dir, "1", "the", "answer"}); err != nil {
			t.Fatalf("cmdAnswer: %v", err)
		}
	})
	if !strings.Contains(out, "answer recorded on task #1") {
		t.Errorf("expected answer recorded, got: %q", out)
	}

	b, err := os.ReadFile(filepath.Join(c.Dir, "tasks", "task-1.md"))
	if err != nil {
		t.Fatalf("task file missing: %v", err)
	}
	content := string(b)
	if !strings.Contains(content, "HUMAN ANSWER: the answer") {
		t.Errorf("task body missing answer, got:\n%s", content)
	}
	if !strings.Contains(content, "status: in_progress") {
		t.Errorf("task status not in_progress, got:\n%s", content)
	}
}

func TestCmdTask_UnknownActionSuggests(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	err := cmdTask([]string{"-C", dir, "ad", "title"})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "did you mean") {
		t.Errorf("expected suggestion, got: %v", err)
	}
}

// TestCmdAnswer_MissingArgs verifies cmdAnswer rejects fewer than two positional args.
func TestCmdAnswer_MissingArgs(t *testing.T) {
	if err := cmdAnswer([]string{}); err == nil {
		t.Fatal("expected error for missing args")
	}
	if err := cmdAnswer([]string{"1"}); err == nil {
		t.Fatal("expected error for missing answer text")
	}
}
