package main

import (
	"os"
	"path/filepath"
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
