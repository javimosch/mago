package main

import (
	"fmt"
	"io"
	"os"
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
