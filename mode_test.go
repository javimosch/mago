package main

import (
	"os"
	"path/filepath"
	"strings"
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
	m, _ = parseMode(base, []string{"comms-on"})
	if !m.Comms {
		t.Errorf("comms-on preset: %+v", m)
	}
	m, _ = parseMode(workerMode{Comms: true}, []string{"comms-off"})
	if m.Comms {
		t.Errorf("comms-off preset: %+v", m)
	}
	m, _ = parseMode(workerMode{Comms: true}, []string{"comms=false"})
	if m.Comms {
		t.Errorf("comms=false: %+v", m)
	}

	// proactive preset should preserve an already-positive cadence.
	m, _ = parseMode(workerMode{Proactive: 1800}, []string{"proactive"})
	if m.Proactive != 1800 {
		t.Errorf("proactive preset should keep existing cadence, got %d", m.Proactive)
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
	t.Setenv("MAGO_PROACTIVE", " 600 ")
	t.Setenv("MAGO_PR_CAP", " 5 ")
	t.Setenv("MAGO_ISSUE_CAP", " 7 ")
	got := c.loadMode()
	if !got.Comms || got.Update != "auto" || got.Merge != "review" ||
		got.Proactive != 600 || got.PRCap != 5 || got.IssueCap != 7 {
		t.Errorf("loadMode did not trim env values: %+v", got)
	}
}

func TestDescribeMode(t *testing.T) {
	cases := []struct {
		m    workerMode
		want string
	}{
		{workerMode{}, "proactive off (reactive) · comms off · merge review"},
		{workerMode{Proactive: 600, Comms: true, Merge: "verified"}, "proactive every 600s · comms on · merge verified"},
		{workerMode{Proactive: 60, Comms: false, Merge: "on", PRCap: 5, IssueCap: 3, Update: "auto"}, "proactive every 60s · comms off · merge on · pr-cap 5 · issue-cap 3 · update auto"},
	}
	for _, tc := range cases {
		if got := describeMode(tc.m); got != tc.want {
			t.Errorf("describeMode(%+v) = %q, want %q", tc.m, got, tc.want)
		}
	}
}

func TestOnOff(t *testing.T) {
	if got := onOff(true); got != "on" {
		t.Errorf("onOff(true) = %q, want on", got)
	}
	if got := onOff(false); got != "off" {
		t.Errorf("onOff(false) = %q, want off", got)
	}
}

func TestModeAccessors(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "co"}
	c.saveMode(workerMode{Proactive: 600, Comms: true, Merge: "verified", PRCap: 5, IssueCap: 3, Update: "auto"})

	if got := c.modeProactive(); got != 600 {
		t.Errorf("modeProactive = %d, want 600", got)
	}
	if !c.modeComms() {
		t.Error("modeComms should be true")
	}
	if got := c.modePRCap(); got != 5 {
		t.Errorf("modePRCap = %d, want 5", got)
	}
	if got := c.modeIssueCap(); got != 3 {
		t.Errorf("modeIssueCap = %d, want 3", got)
	}
	if got := c.modeUpdate(); got != "auto" {
		t.Errorf("modeUpdate = %q, want auto", got)
	}

	// manual/normalized update
	c.saveMode(workerMode{Update: "manual"})
	if got := c.modeUpdate(); got != "manual" {
		t.Errorf("modeUpdate = %q, want manual", got)
	}
}

func TestApplyControl(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "co"}

	c.applyControl([]byte(`{"tokens":["proactive=1200","comms=on","merge=verified","update=auto"]}`))

	m := c.loadMode()
	if m.Proactive != 1200 || !m.Comms || m.Merge != "verified" || m.Update != "auto" {
		t.Errorf("applyControl did not persist mode: %+v", m)
	}

	c.applyControl([]byte(`{not json`))
	m = c.loadMode()
	if m.Proactive != 1200 || !m.Comms || m.Merge != "verified" || m.Update != "auto" {
		t.Errorf("invalid JSON should not overwrite mode: %+v", m)
	}

	c.applyControl([]byte(`{"tokens":[]}`))
	m = c.loadMode()
	if m.Proactive != 1200 || !m.Comms || m.Merge != "verified" || m.Update != "auto" {
		t.Errorf("empty tokens should not overwrite mode: %+v", m)
	}

	c.applyControl([]byte(`{"tokens":["bogus"]}`))
	m = c.loadMode()
	if m.Proactive != 1200 || !m.Comms || m.Merge != "verified" || m.Update != "auto" {
		t.Errorf("rejected tokens should not overwrite mode: %+v", m)
	}
}

// TestCmdMode_ShowAndSet verifies the local `mago mode` command displays the current
// mode and persists changes after applying tokens.
func TestCmdMode_ShowAndSet(t *testing.T) {
	c := newTestCompany(t)
	t.Setenv("MAGO_COMPANY", "")
	t.Setenv("MAGO_GH_REPO", "")
	t.Setenv("MAGO_NO_MERGE", "1")
	t.Setenv("MAGO_VERIFY", "")
	t.Setenv("MAGO_VERIFY_CMD", "")
	t.Setenv("MAGO_COMMS", "")
	t.Setenv("MAGO_PROACTIVE", "")
	t.Setenv("MAGO_PR_CAP", "")
	t.Setenv("MAGO_ISSUE_CAP", "")
	t.Setenv("MAGO_UPDATE", "")

	out := captureStdout(t, func() {
		if err := cmdMode([]string{"-C", c.Dir, "show"}); err != nil {
			t.Fatalf("cmdMode show: %v", err)
		}
	})
	if !strings.Contains(out, "proactive off (react") {
		t.Errorf("show output missing default proactive: %q", out)
	}
	if !strings.Contains(out, "merge review") {
		t.Errorf("show output missing default merge=review: %q", out)
	}

	out = captureStdout(t, func() {
		if err := cmdMode([]string{"-C", c.Dir, "proactive=600", "comms=on", "merge=verified", "update=auto"}); err != nil {
			t.Fatalf("cmdMode set: %v", err)
		}
	})
	if !strings.Contains(out, "proactive every 600s") {
		t.Errorf("set output missing proactive: %q", out)
	}
	if !strings.Contains(out, "comms on") {
		t.Errorf("set output missing comms: %q", out)
	}
	if !strings.Contains(out, "merge verified") {
		t.Errorf("set output missing merge: %q", out)
	}
	if !strings.Contains(out, "update auto") {
		t.Errorf("set output missing update auto: %q", out)
	}

	m := c.loadMode()
	if m.Proactive != 600 || !m.Comms || m.Merge != "verified" || m.Update != "auto" {
		t.Errorf("persisted mode mismatch: %+v", m)
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
