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

// TestParseCompanyDir_WhitespaceEnvIsTrimmed verifies $MAGO_COMPANY is trimmed so a
// whitespace-only value falls back to the cwd, and surrounding spaces around a real
// directory are ignored (matching the -C value handling above).
func TestParseCompanyDir_WhitespaceEnvIsTrimmed(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "   ")
	dir, rest, err := parseCompanyDir([]string{"status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "." {
		t.Errorf("whitespace-only MAGO_COMPANY should fall back to cwd, got dir=%q", dir)
	}

	t.Setenv("MAGO_COMPANY", "  /srv/acme  ")
	dir, rest, err = parseCompanyDir([]string{"status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/srv/acme" {
		t.Errorf("padded MAGO_COMPANY should be trimmed, got dir=%q", dir)
	}
	if len(rest) != 1 || rest[0] != "status" {
		t.Errorf("rest = %v, want [status]", rest)
	}
}

// TestParseCompanyDir_WhitespaceFlagIsTrimmed verifies the -C <dir> value is trimmed so
// surrounding spaces are ignored and the flag still sets the company directory correctly.
func TestParseCompanyDir_WhitespaceFlagIsTrimmed(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")

	dir, rest, err := parseCompanyDir([]string{"-C", "  /srv/acme  ", "status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dir != "/srv/acme" {
		t.Errorf("padded -C value should be trimmed, got dir=%q", dir)
	}
	if len(rest) != 1 || rest[0] != "status" {
		t.Errorf("rest = %v, want [status]", rest)
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

func TestCmdStatus_WithProject(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	if err := cmdProject([]string{"-C", dir, "add", "web", "--repo", "acme/web"}); err != nil {
		t.Fatalf("cmdProject web: %v", err)
	}
	if err := cmdProject([]string{"-C", dir, "add", "api", "--repo", "acme/api"}); err != nil {
		t.Fatalf("cmdProject api: %v", err)
	}

	out := captureStdout(t, func() {
		if err := cmdStatus([]string{"-C", dir}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})

	if !strings.Contains(out, "## Projects") {
		t.Errorf("expected projects section, got: %q", out)
	}
	if !strings.Contains(out, "web -> acme/web") || !strings.Contains(out, "api -> acme/api") {
		t.Errorf("expected projects in status output, got: %q", out)
	}
}

// TestCmdStatus_PendingHITL verifies that a task with an open human question shows up
// in the "## Pending human input (HITL)" section of `mago status`.
func TestCmdStatus_PendingHITL(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	if err := cmdTask([]string{"-C", dir, "add", "blocked on ceo"}); err != nil {
		t.Fatalf("cmdTask: %v", err)
	}

	c := &Company{Dir: dir, Name: "test"}
	c.tasks = &localBackend{c: c}
	task, err := c.tasks.FindTask("1")
	if err != nil {
		t.Fatalf("FindTask: %v", err)
	}
	if err := c.tasks.RaiseHITL(task, "cto", "Ship the CLI as a single binary or a tarball?"); err != nil {
		t.Fatalf("RaiseHITL: %v", err)
	}

	out := captureStdout(t, func() {
		if err := cmdStatus([]string{"-C", dir}); err != nil {
			t.Fatalf("cmdStatus: %v", err)
		}
	})

	if !strings.Contains(out, "## Pending human input (HITL)") {
		t.Errorf("expected HITL section, got: %q", out)
	}
	if !strings.Contains(out, "single binary or a tarball") {
		t.Errorf("expected the pending question text, got: %q", out)
	}
	if !strings.Contains(out, "[needs_human]") {
		t.Errorf("expected the task's needs_human status in the list, got: %q", out)
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

// TestCmdAnswer_TaskNotFound verifies cmdAnswer surfaces the backend's "not found"
// error instead of silently succeeding or crashing.
func TestCmdAnswer_TaskNotFound(t *testing.T) {
	c := newTestCompany(t)
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")

	if err := cmdAnswer([]string{"-C", c.Dir, "99", "nope"}); err == nil {
		t.Fatal("expected error for missing task")
	}
}

// TestCmdTask_BareFlagRejected verifies `mago task -C` with no directory value is
// rejected by parseCompanyDir rather than silently treated as a task action.
func TestCmdTask_BareFlagRejected(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	err := cmdTask([]string{"-C"})
	if err == nil {
		t.Fatal("expected error for bare -C")
	}
	if !strings.Contains(err.Error(), "-C") {
		t.Errorf("error = %q, want mention of -C", err.Error())
	}
}

// TestCmdTask_NoAction verifies `mago task` with no sub-action fails with the
// add usage line.
func TestCmdTask_NoAction(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	err := cmdTask([]string{"-C", t.TempDir()})
	if err == nil {
		t.Fatal("expected usage error for missing action")
	}
	if !strings.Contains(err.Error(), "usage: mago task add") {
		t.Errorf("error = %q, want task add usage", err.Error())
	}
}

// TestCmdTask_UnknownActionNoSuggestion verifies an unrelated action name produces the
// plain "unknown task action" error — nearestAction should not offer a misleading
// "did you mean" for input nothing is close to.
func TestCmdTask_UnknownActionNoSuggestion(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	err := cmdTask([]string{"-C", t.TempDir(), "zzz"})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), `unknown task action "zzz"`) {
		t.Errorf("error = %q, want unknown action", err.Error())
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error = %q, want no suggestion for unrelated input", err.Error())
	}
}

// TestCmdTask_NotACompany verifies cmdTask surfaces the loadCompany error when -C
// points at a directory with no .mago/.
func TestCmdTask_NotACompany(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	err := cmdTask([]string{"-C", t.TempDir(), "add", "x"})
	if err == nil {
		t.Fatal("expected error for directory without .mago/")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error = %q, want 'not a mago company'", err.Error())
	}
}

// TestCmdProject_BareFlagRejected verifies `mago project -C` with no directory value is
// rejected by parseCompanyDir.
func TestCmdProject_BareFlagRejected(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	err := cmdProject([]string{"-C"})
	if err == nil {
		t.Fatal("expected error for bare -C")
	}
	if !strings.Contains(err.Error(), "-C") {
		t.Errorf("error = %q, want mention of -C", err.Error())
	}
}

// TestCmdProject_NotACompany verifies cmdProject surfaces the loadCompany error when -C
// points at a directory with no .mago/.
func TestCmdProject_NotACompany(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	err := cmdProject([]string{"-C", t.TempDir(), "list"})
	if err == nil {
		t.Fatal("expected error for directory without .mago/")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error = %q, want 'not a mago company'", err.Error())
	}
}

// TestCmdProject_UnknownActionNoSuggestion verifies an unrelated action name produces the
// plain "unknown project action" error with no misleading "did you mean" suggestion.
func TestCmdProject_UnknownActionNoSuggestion(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)

	err := cmdProject([]string{"-C", dir, "zzz"})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), `unknown project action "zzz"`) {
		t.Errorf("error = %q, want unknown action", err.Error())
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("error = %q, want no suggestion for unrelated input", err.Error())
	}
}

// TestCmdProject_AddWithMirror verifies `mago project add --mirror` persists the
// mirror_issue flag and reports it in both the add output and `project list`.
func TestCmdProject_AddWithMirror(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	os.MkdirAll(filepath.Join(dir, "tasks"), 0o755)

	out := captureStdout(t, func() {
		if err := cmdProject([]string{"-C", dir, "add", "web", "--repo", "acme/web", "--mirror"}); err != nil {
			t.Fatalf("cmdProject: %v", err)
		}
	})
	if !strings.Contains(out, `project "web" ready -> acme/web`) {
		t.Errorf("unexpected add output: %q", out)
	}
	if !strings.Contains(out, "[mirror-issue on]") {
		t.Errorf("expected mirror-issue marker in add output, got: %q", out)
	}

	out = captureStdout(t, func() {
		if err := cmdProject([]string{"-C", dir, "list"}); err != nil {
			t.Fatalf("cmdProject list: %v", err)
		}
	})
	if !strings.Contains(out, "web -> acme/web") || !strings.Contains(out, "[mirror-issue]") {
		t.Errorf("expected mirrored project in list, got: %q", out)
	}
}

// TestCmdStatus_BareFlagRejected verifies `mago status -C` with no directory value is
// rejected by parseCompanyDir.
func TestCmdStatus_BareFlagRejected(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	err := cmdStatus([]string{"-C"})
	if err == nil {
		t.Fatal("expected error for bare -C")
	}
	if !strings.Contains(err.Error(), "-C") {
		t.Errorf("error = %q, want mention of -C", err.Error())
	}
}

// TestCmdStatus_NotACompany verifies cmdStatus surfaces the loadCompany error when -C
// points at a directory with no .mago/.
func TestCmdStatus_NotACompany(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	err := cmdStatus([]string{"-C", t.TempDir()})
	if err == nil {
		t.Fatal("expected error for directory without .mago/")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error = %q, want 'not a mago company'", err.Error())
	}
}

// TestCmdAnswer_BareFlagRejected verifies `mago answer -C` with no directory value is
// rejected by parseCompanyDir.
func TestCmdAnswer_BareFlagRejected(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	err := cmdAnswer([]string{"-C"})
	if err == nil {
		t.Fatal("expected error for bare -C")
	}
	if !strings.Contains(err.Error(), "-C") {
		t.Errorf("error = %q, want mention of -C", err.Error())
	}
}

// TestCmdAnswer_NotACompany verifies cmdAnswer surfaces the loadCompany error when -C
// points at a directory with no .mago/.
func TestCmdAnswer_NotACompany(t *testing.T) {
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	err := cmdAnswer([]string{"-C", t.TempDir(), "1", "x"})
	if err == nil {
		t.Fatal("expected error for directory without .mago/")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error = %q, want 'not a mago company'", err.Error())
	}
}
