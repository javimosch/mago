package main

import (
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

	t.Run("flash model warns", func(t *testing.T) {
		t.Setenv("MAGO_PROVIDER", "opencode-go")
		t.Setenv("MAGO_MODEL", "openrouter/flash-v1")
		t.Setenv("OPENCODE_API_KEY", "secret")
		out := capture(warnIfNoProviderKey)
		if !strings.Contains(out, "flash variant") {
			t.Errorf("flash model stderr = %q, want flash warning", out)
		}
	})
}

// installFakeTau writes a shell script named "tau" into a temp dir and prepends that
// dir to PATH so tauComplete/recoverReflection can be exercised without a real tau.
func installFakeTau(t *testing.T, output string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "tau")
	body := "#!/bin/sh\nprintf '%s\\n' '" + output + "'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake tau: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRecoverReflection_Success(t *testing.T) {
	c := newTestCompany(t)
	a := &Agent{Provider: "tau", Model: "test"}
	task := &Task{ID: "1", Title: "test task"}
	ws := t.TempDir()

	output := `{"content": "{\"summary\":\"recovered summary\",\"state_delta\":\"\",\"task_status\":\"in_progress\",\"next\":\"continue\",\"cadence_signal\":\"working\"}"}`
	installFakeTau(t, output)

	r := c.recoverReflection(a, task, ws)
	if r == nil {
		t.Fatal("recoverReflection should return a reflection on valid output")
	}
	if r.Summary != "recovered summary" {
		t.Errorf("summary = %q, want %q", r.Summary, "recovered summary")
	}
	if r.TaskStatus != "in_progress" {
		t.Errorf("task_status = %q, want in_progress", r.TaskStatus)
	}
}

func TestRecoverReflection_ParseFailure(t *testing.T) {
	c := newTestCompany(t)
	a := &Agent{Provider: "tau", Model: "test"}
	task := &Task{ID: "2", Title: "test task"}
	ws := t.TempDir()

	// tau returns prose with no parseable reflection JSON.
	installFakeTau(t, `{"content": "not a reflection"}`)

	r := c.recoverReflection(a, task, ws)
	if r != nil {
		t.Errorf("recoverReflection should return nil for unparseable content, got %+v", r)
	}
}

func TestRecoverReflection_SelfHealHook(t *testing.T) {
	c := newTestCompany(t)
	a := &Agent{Provider: "tau", Model: "test"}
	task := &Task{ID: "3", Title: "test task"}
	ws := t.TempDir()

	// Even if the fake tau would produce a valid reflection, the forced-failure hook
	// short-circuits the recovery so runTick falls through to the self-heal path.
	t.Setenv("MAGO_TEST_BAD_REFLECTION", "2")
	installFakeTau(t, `{"content": "{\"summary\":\"ignored\"}"}`)

	r := c.recoverReflection(a, task, ws)
	if r != nil {
		t.Errorf("MAGO_TEST_BAD_REFLECTION=2 should force nil, got %+v", r)
	}
}
