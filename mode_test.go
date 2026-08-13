package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMode(t *testing.T) {
	base := workerMode{Proactive: 0, Comms: false, Merge: "review"}

	m, err := parseMode(base, []string{"proactive"})
	if err != nil || m.Proactive != 3600 {
		t.Fatalf("proactive preset: %+v %v", m, err)
	}
	m, _ = parseMode(base, []string{"proactive=1800", "comms=on", "merge=verified"})
	if m.Proactive != 1800 || !m.Comms || m.Merge != "verified" {
		t.Errorf("kv apply: %+v", m)
	}
	m, _ = parseMode(workerMode{Proactive: 1800, Comms: true, Merge: "verified"}, []string{"reactive"})
	if m.Proactive != 0 || !m.Comms || m.Merge != "verified" {
		t.Errorf("reactive should only zero proactive, kept rest: %+v", m)
	}
	if _, err := parseMode(base, []string{"bogus"}); err == nil {
		t.Error("unknown token should error")
	}
	if _, err := parseMode(base, []string{"merge=sometimes"}); err == nil {
		t.Error("bad merge value should error")
	}
	m, _ = parseMode(base, []string{"pr-cap=10", "issue-cap=5"})
	if m.PRCap != 10 || m.IssueCap != 5 {
		t.Errorf("cap kv apply: %+v", m)
	}
	m, _ = parseMode(workerMode{PRCap: 10, IssueCap: 5, Merge: "review"}, []string{"pr-cap=0"})
	if m.PRCap != 0 || m.IssueCap != 5 {
		t.Errorf("pr-cap=0 should clear only PRCap: %+v", m)
	}
	m, _ = parseMode(base, []string{"update=auto"})
	if m.Update != "auto" {
		t.Errorf("update=auto: %+v", m)
	}
	if _, err := parseMode(base, []string{"update=sometimes"}); err == nil {
		t.Error("bad update value should error")
	}
}

func TestLoadModeTrimsEnv(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "co"}

	t.Setenv("MAGO_COMMS", " 1 ")
	t.Setenv("MAGO_UPDATE", " auto ")
	t.Setenv("MAGO_NO_MERGE", " 1 ")
	if got := c.loadMode(); !got.Comms || got.Update != "auto" || got.Merge != "review" {
		t.Errorf("loadMode did not trim env values: %+v", got)
	}
}

func TestLoadSaveMode(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "co"}

	// env fallback when no file
	os.Unsetenv("MAGO_PROACTIVE")
	t.Setenv("MAGO_NO_MERGE", "1")
	if got := c.loadMode(); got.Merge != "review" {
		t.Errorf("env fallback merge = %q, want review", got.Merge)
	}
	// persisted file wins + verifyEnabled tracks merge mode
	c.saveMode(workerMode{Proactive: 600, Comms: true, Merge: "verified"})
	if got := c.loadMode(); got.Proactive != 600 || !got.Comms || got.Merge != "verified" {
		t.Errorf("loaded %+v", got)
	}
	if !c.verifyEnabled() {
		t.Error("verified mode should enable verification")
	}
	c.saveMode(workerMode{Merge: "review"})
	if c.verifyEnabled() {
		t.Error("review mode should not verify")
	}
}
