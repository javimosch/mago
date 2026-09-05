package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyModelOverrides(t *testing.T) {
	t.Run("provider override", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "pi")
		t.Setenv("MAGO_MODEL", "")
		a := &Agent{Provider: "tau", Model: "deepseek-v4"}
		applyModelOverrides(a)
		if a.Provider != "pi" {
			t.Fatalf("Provider = %q, want %q", a.Provider, "pi")
		}
		if a.Model != "deepseek-v4" {
			t.Fatalf("Model was overwritten when MAGO_MODEL empty")
		}
	})

	t.Run("model override", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "")
		t.Setenv("MAGO_MODEL", "openai/gpt-4")
		a := &Agent{Provider: "tau", Model: "deepseek-v4"}
		applyModelOverrides(a)
		if a.Provider != "tau" {
			t.Fatalf("Provider was overwritten when MAGO_PROVIDER empty")
		}
		if a.Model != "openai/gpt-4" {
			t.Fatalf("Model = %q, want %q", a.Model, "openai/gpt-4")
		}
	})

	t.Run("padded provider is trimmed", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "  pi  ")
		t.Setenv("MAGO_MODEL", "")
		a := &Agent{Provider: "tau", Model: "deepseek-v4"}
		applyModelOverrides(a)
		if a.Provider != "pi" {
			t.Fatalf("Provider = %q, want %q", a.Provider, "pi")
		}
	})

	t.Run("padded model is trimmed", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "")
		t.Setenv("MAGO_MODEL", "  openai/gpt-4  ")
		a := &Agent{Provider: "tau", Model: "deepseek-v4"}
		applyModelOverrides(a)
		if a.Model != "openai/gpt-4" {
			t.Fatalf("Model = %q, want %q", a.Model, "openai/gpt-4")
		}
	})
}

func TestTauConfigHasKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	t.Run("missing config returns false", func(t *testing.T) {
		if tauConfigHasKey("opencode-go") {
			t.Fatal("tauConfigHasKey without config = true, want false")
		}
	})

	t.Run("api_key fallback", func(t *testing.T) {
		cfg := filepath.Join(dir, ".config", "tau")
		ensureDir(cfg)
		os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"api_key":"secret"}`), 0o600)
		if !tauConfigHasKey("opencode-go") {
			t.Fatal("tauConfigHasKey with api_key = false, want true")
		}
	})

	t.Run("per-provider key", func(t *testing.T) {
		cfg := filepath.Join(dir, ".config", "tau")
		ensureDir(cfg)
		os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"keys":{"opencode-go":"secret"}}`), 0o600)
		if !tauConfigHasKey("opencode-go") {
			t.Fatal("tauConfigHasKey with per-provider key = false, want true")
		}
	})

	t.Run("malformed config returns false", func(t *testing.T) {
		cfg := filepath.Join(dir, ".config", "tau")
		ensureDir(cfg)
		os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{not json`), 0o600)
		if tauConfigHasKey("opencode-go") {
			t.Fatal("tauConfigHasKey with malformed config = true, want false")
		}
	})

	t.Run("config for another provider returns false", func(t *testing.T) {
		cfg := filepath.Join(dir, ".config", "tau")
		ensureDir(cfg)
		os.WriteFile(filepath.Join(cfg, "config.json"), []byte(`{"keys":{"deepseek":"secret"}}`), 0o600)
		if tauConfigHasKey("opencode-go") {
			t.Fatal("tauConfigHasKey for missing provider = true, want false")
		}
	})
}

func TestRunTick_NoTask(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Developer\n---\n")

	res, err := runTick(c, "dev")
	if err != nil {
		t.Fatalf("runTick error: %v", err)
	}
	if res.worked {
		t.Error("runTick with no actionable task should report no work")
	}
	if res.signal != "idle" {
		t.Errorf("signal = %q, want idle", res.signal)
	}
}

// TestRunTick_ReviewerBouncesTask verifies that a review-only agent never claims an
// issue-task: it bounces the task and reports work so it can be re-routed to an implementer.
func TestRunTick_ReviewerBouncesTask(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "rev", "---\nname: rev\ntitle: Head of Org Engineering\nreviews: true\n---\n")

	task, err := c.tasks.AddTask("Triage the review backlog", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := c.tasks.Assign(task, "rev"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	res, err := runTick(c, "rev")
	if err != nil {
		t.Fatalf("runTick error: %v", err)
	}
	if !res.worked {
		t.Error("reviewer bounce should report worked=true")
	}
	if res.signal != "working" {
		t.Errorf("signal = %q, want working", res.signal)
	}

	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(ts) != 1 {
		t.Fatalf("expected 1 task, got %d", len(ts))
	}
	if ts[0].Assignee != "" {
		t.Errorf("bounced task should be unassigned, got %q", ts[0].Assignee)
	}
	if ts[0].Status != "open" {
		t.Errorf("bounced task should be open, got %q", ts[0].Status)
	}
}

func TestWarnIfNoProviderKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	capture := func(f func()) string {
		old := os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stderr = w
		f()
		w.Close()
		os.Stderr = old
		b, _ := io.ReadAll(r)
		return string(b)
	}

	t.Run("unknown provider is silent", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "unknown")
		t.Setenv("MAGO_MODEL", "")
		out := capture(warnIfNoProviderKey)
		if out != "" {
			t.Errorf("unknown provider produced stderr: %q", out)
		}
	})

	t.Run("env key present is silent", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "opencode-go")
		t.Setenv("MAGO_MODEL", "")
		t.Setenv("OPENCODE_API_KEY", "secret")
		out := capture(warnIfNoProviderKey)
		if out != "" {
			t.Errorf("env key present produced stderr: %q", out)
		}
	})

	t.Run("missing key warns", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "opencode-go")
		t.Setenv("MAGO_MODEL", "")
		t.Setenv("OPENCODE_API_KEY", "")
		out := capture(warnIfNoProviderKey)
		if !strings.Contains(out, "no API key for provider") {
			t.Errorf("missing key stderr = %q, want key warning", out)
		}
		if !strings.Contains(out, "OPENCODE_API_KEY") {
			t.Errorf("missing key stderr should name OPENCODE_API_KEY: %q", out)
		}
	})

	t.Run("whitespace-only env key warns", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "opencode-go")
		t.Setenv("MAGO_MODEL", "")
		t.Setenv("OPENCODE_API_KEY", "   ")
		out := capture(warnIfNoProviderKey)
		if !strings.Contains(out, "no API key for provider") {
			t.Errorf("whitespace-only key stderr = %q, want key warning", out)
		}
	})

	t.Run("flash model warns", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "opencode-go")
		t.Setenv("MAGO_MODEL", "openrouter/flash-v1")
		t.Setenv("OPENCODE_API_KEY", "secret")
		out := capture(warnIfNoProviderKey)
		if !strings.Contains(out, "flash variant") {
			t.Errorf("flash model stderr = %q, want flash warning", out)
		}
	})

	t.Run("padded provider still maps to key", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "  opencode-go  ")
		t.Setenv("MAGO_MODEL", "")
		t.Setenv("OPENCODE_API_KEY", "")
		out := capture(warnIfNoProviderKey)
		if !strings.Contains(out, "no API key for provider") {
			t.Errorf("missing key stderr = %q, want key warning", out)
		}
	})

	t.Run("padded flash model still warns", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "opencode-go")
		t.Setenv("MAGO_MODEL", "  openrouter/flash-v1  ")
		t.Setenv("OPENCODE_API_KEY", "secret")
		out := capture(warnIfNoProviderKey)
		if !strings.Contains(out, "flash variant") {
			t.Errorf("flash model stderr = %q, want flash warning", out)
		}
	})
}

// TestRecoverReflection_Success verifies the reflection-recovery path can ask the
// harness for a clean reflection and parse it when the original output was malformed.
func TestRecoverReflection_Success(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "claude")

	// claude --output-format json emits a JSON result whose "result" string is itself
	// a valid reflection JSON object.
	reflection := `{"summary":"ok","task_status":"done","next":"","cadence_signal":"idle"}`
	escaped := strings.ReplaceAll(reflection, `"`, `\"`)
	body := fmt.Sprintf(`#!/bin/sh
	cat >/dev/null 2>/dev/null
	echo '{"result": "%s", "is_error": false, "subtype": ""}'
`, escaped)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	c := &Company{Dir: t.TempDir(), Name: "co"}
	task := &Task{ID: "1", Title: "fix it"}
	ws := t.TempDir()

	got := c.recoverReflection(&Agent{Provider: "claude"}, task, ws)
	if got == nil {
		t.Fatal("recoverReflection returned nil, want a reflection")
	}
	if got.Summary != "ok" {
		t.Errorf("summary = %q, want ok", got.Summary)
	}
}

// TestRunTick_UnknownAgent verifies runTick surfaces the loadAgent error when the
// named agent has no definition file, instead of starting a tick.
func TestRunTick_UnknownAgent(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}

	_, err := runTick(c, "ghost")
	if err == nil {
		t.Fatal("runTick for an unknown agent should error")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should name the agent, got: %v", err)
	}
}

// TestRunTick_TauStartFails verifies that a missing tau binary propagates as an
// error (not a crash) after the task is claimed — the tick reports the failure so
// the caller can surface a 100-class integration error.
func TestRunTick_TauStartFails(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_PROVIDER", "")
	t.Setenv("MAGO_MODEL", "")
	t.Setenv("MAGO_TEST_BAD_REFLECTION", "")
	t.Setenv("PATH", t.TempDir()) // guarantee no tau binary is found

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Developer\nimplements: true\n---\n")

	task, err := c.tasks.AddTask("Do the work", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := c.tasks.Assign(task, "dev"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	_, err = runTick(c, "dev")
	if err == nil {
		t.Fatal("runTick should error when tau cannot be started")
	}
	if !strings.Contains(err.Error(), "tau") {
		t.Errorf("error should mention tau, got: %v", err)
	}
}

// fakeClaude writes a fake `claude` binary on PATH that emits a single
// `{"result": "<result>", "is_error": false}` JSON object for every invocation —
// enough for claudeComplete (reflection recovery) and runClaude-free test paths.
func fakeClaude(t *testing.T, result string) {
	t.Helper()
	dir := t.TempDir()
	escaped := strings.ReplaceAll(result, `"`, `\"`)
	body := fmt.Sprintf(`#!/bin/sh
cat >/dev/null 2>/dev/null
echo '{"result": "%s", "is_error": false, "subtype": ""}'
`, escaped)
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

// TestRunTick_RecoveredReflection verifies the recovery path end-to-end: when the
// tick's main output is unparseable (MAGO_TEST_BAD_REFLECTION=1) the follow-up
// no-tools call salvages a reflection and writeBack applies it normally.
func TestRunTick_RecoveredReflection(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_PROVIDER", "")
	t.Setenv("MAGO_MODEL", "")
	t.Setenv("MAGO_TEST_BAD_REFLECTION", "1") // bad main output, recovery succeeds
	fakeClaude(t, `{"summary":"polished the copy","task_status":"done","next":"","cadence_signal":"idle"}`)

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Developer\nprovider: claude\nimplements: true\n---\n")

	task, err := c.tasks.AddTask("Polish the README", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := c.tasks.Assign(task, "dev"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	res, err := runTick(c, "dev")
	if err != nil {
		t.Fatalf("runTick error: %v", err)
	}
	if !res.worked {
		t.Error("recovered tick should report worked=true")
	}
	if res.status != "done" {
		t.Errorf("status = %q, want done", res.status)
	}

	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if ts[0].Status != "done" {
		t.Errorf("task status = %q, want done", ts[0].Status)
	}
	journals, err := filepath.Glob(filepath.Join(c.runsDir(), "dev", "*.json"))
	if err != nil || len(journals) == 0 {
		t.Fatalf("expected a run journal under .mago/runs/dev, got %v (err %v)", journals, err)
	}
}

// TestRunTick_DoneWithoutPRStaysInProgress verifies the implementer guard: a
// project task whose reflection claims "done" but has no PR on the project repo
// is kept in_progress with a Next that walks the agent through opening the PR.
// A real local git origin stands in for the project repo; the fake gh answers
// `repo clone` (clones the origin), `repo view` (default branch) and `pr list`
// (empty — nothing shipped).
func TestRunTick_DoneWithoutPRStaysInProgress(t *testing.T) {
	origin := t.TempDir()
	gitIn := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitIn(origin, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(origin, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(origin, "add", ".")
	gitIn(origin, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "init")

	bindir := t.TempDir()
	fakeGH := `#!/bin/sh
if [ "$1" = "repo" ] && [ "$2" = "clone" ]; then
  exec git clone -q "file://$MAGO_TEST_ORIGIN" "$4"
fi
if [ "$1" = "repo" ] && [ "$2" = "view" ]; then
  echo main
  exit 0
fi
echo '[]'
`
	if err := os.WriteFile(filepath.Join(bindir, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_PROVIDER", "")
	t.Setenv("MAGO_MODEL", "")
	t.Setenv("MAGO_TEST_BAD_REFLECTION", "1")
	t.Setenv("MAGO_TEST_ORIGIN", origin)
	t.Setenv("PATH", bindir+":"+os.Getenv("PATH"))
	fakeClaude(t, `{"summary":"shipped it","task_status":"done","next":"","cadence_signal":"idle"}`)

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Developer\nprovider: claude\nimplements: true\n---\n")
	if err := os.WriteFile(c.projectsConfigFile(), []byte(`{"web":"acme/web"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	task, err := c.tasks.AddTask("Ship the feature", "web")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := c.tasks.Assign(task, "dev"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	res, err := runTick(c, "dev")
	if err != nil {
		t.Fatalf("runTick error: %v", err)
	}
	if res.status != "in_progress" {
		t.Errorf("status = %q, want in_progress (done rejected: no PR shipped)", res.status)
	}

	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if ts[0].Status != "in_progress" {
		t.Errorf("task status = %q, want in_progress", ts[0].Status)
	}
	if !strings.Contains(ts[0].Body, "gh pr create") {
		t.Errorf("task body should carry the open-the-PR instructions, got:\n%s", ts[0].Body)
	}
}

// TestRunTick_BadReflectionSelfHeals verifies the no-parseable-reflection path:
// the raw output is saved, the task stays claimed (in_progress) for the next
// tick, and the tick still reports work so the loop doesn't stall.
func TestRunTick_BadReflectionSelfHeals(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_TEST_BAD_REFLECTION", "2") // recovery ALSO fails

	c := newTestCompany(t)
	c.tasks = &localBackend{c: c}
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Developer\nimplements: true\n---\n")

	task, err := c.tasks.AddTask("Write the report", "")
	if err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := c.tasks.Assign(task, "dev"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	res, err := runTick(c, "dev")
	if err != nil {
		t.Fatalf("runTick error: %v", err)
	}
	if !res.worked {
		t.Error("self-heal path should report worked=true")
	}
	if res.signal != "working" {
		t.Errorf("signal = %q, want working", res.signal)
	}

	raws, err := filepath.Glob(filepath.Join(c.runsDir(), "dev", "*-RAW.txt"))
	if err != nil || len(raws) == 0 {
		t.Fatalf("expected a saved *-RAW.txt under .mago/runs/dev, got %v (err %v)", raws, err)
	}

	ts, err := c.tasks.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if ts[0].Status != "in_progress" || ts[0].Assignee != "dev" {
		t.Errorf("task should stay claimed for the next tick, got status=%q assignee=%q", ts[0].Status, ts[0].Assignee)
	}
}

// TestCmdRun_MissingArgs verifies cmdRun surfaces usage when no agent is given.
func TestCmdRun_MissingArgs(t *testing.T) {
	err := cmdRun([]string{})
	if err == nil {
		t.Fatal("cmdRun with no args should error")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("error = %q, want usage message", err.Error())
	}
}

// TestCmdRun_Idle verifies cmdRun prints the idle message when no task is ready.
func TestCmdRun_Idle(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	c := newTestCompany(t)
	writeAgentFile(t, c, "dev", "---\nname: dev\ntitle: Developer\n---\n")

	out := captureStdout(t, func() {
		if err := cmdRun([]string{"-C", c.Dir, "dev"}); err != nil {
			t.Fatalf("cmdRun error: %v", err)
		}
	})
	if !strings.Contains(out, "no actionable tasks") {
		t.Errorf("output = %q, want idle message", out)
	}
}
