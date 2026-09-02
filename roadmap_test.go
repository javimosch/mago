package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMdSection(t *testing.T) {
	md := "# T\n\n## Now\nShip the relay.\nAnd caps.\n\n## Next\nlater stuff\n\n## Out of scope\nrewrites\n"
	if got := mdSection(md, "Now"); got != "Ship the relay.\nAnd caps." {
		t.Errorf("Now = %q", got)
	}
	if got := mdSection(md, "out of scope"); got != "rewrites" { // case-insensitive
		t.Errorf("Out of scope = %q", got)
	}
	if got := mdSection(md, "Missing"); got != "" {
		t.Errorf("absent section should be empty, got %q", got)
	}
}

func TestIsPlaceholder(t *testing.T) {
	for _, s := range []string{"", "  ", "(set me)", "(The current focus…)"} {
		if !isPlaceholder(s) {
			t.Errorf("%q should be placeholder", s)
		}
	}
	if isPlaceholder("Ship the thing") {
		t.Error("real text should not be a placeholder")
	}
}

func TestPlanningFocusFallback(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "co"}

	// No ROADMAP.md, mission set in STATE.md -> fall back to mission.
	// (parseStateSections needs all of Mission/Shipped/In flight/Decisions present.)
	os.WriteFile(c.stateFile(), []byte("## Mission\nMission focus.\n\n## Shipped\n(nothing yet)\n\n## In flight\n(nothing yet)\n\n## Decisions\n(none yet)\n\n## Activity log\n"), 0o644)
	if got := c.planningFocus(); got != "Mission focus." {
		t.Errorf("fallback to mission failed: %q", got)
	}

	// ROADMAP.md with a real ## Now wins over the mission.
	os.WriteFile(c.roadmapFile(), []byte("## Now\nRoadmap focus.\n\n## Out of scope\nbig rewrites\n"), 0o644)
	if got := c.planningFocus(); got != "Roadmap focus." {
		t.Errorf("roadmap Now should win: %q", got)
	}

	// Placeholder ## Now -> still fall back to mission.
	os.WriteFile(c.roadmapFile(), []byte("## Now\n(placeholder)\n"), 0o644)
	if got := c.planningFocus(); got != "Mission focus." {
		t.Errorf("placeholder Now should fall back to mission: %q", got)
	}

	// directionContext picks up vision + out-of-scope.
	os.WriteFile(c.visionFile(), []byte("## North star\nRun companies.\n\n## Constraints\nNo core edits.\n"), 0o644)
	os.WriteFile(c.roadmapFile(), []byte("## Now\nShip X.\n\n## Out of scope\nrewrites\n"), 0o644)
	dc := c.directionContext()
	for _, want := range []string{"Run companies.", "Ship X.", "No core edits.", "rewrites"} {
		if !strings.Contains(dc, want) {
			t.Errorf("directionContext missing %q:\n%s", want, dc)
		}
	}
}

// TestAdvanceRoadmapNoLater verifies the rotation when the roadmap has no ## Later
// section: Now <- Next, Next becomes the placeholder, and the old Now is archived.
func TestAdvanceRoadmapNoLater(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "co"}

	os.WriteFile(c.roadmapFile(), []byte(
		"# co — ROADMAP\n\n## Now\nShip A.\n\n## Next\nShip B.\n\n## Out of scope\nrewrites\n"), 0o644)

	if !c.advanceRoadmap() {
		t.Fatal("advanceRoadmap should succeed")
	}
	if got := c.roadmapNow(); got != "Ship B." {
		t.Errorf("Now = %q, want %q", got, "Ship B.")
	}
	if got := c.roadmapNext(); got != "(none yet)" {
		t.Errorf("Next = %q, want %q", got, "(none yet)")
	}
	raw := c.roadmapRaw()
	if !strings.Contains(raw, "## Done") || !strings.Contains(raw, "Ship A.") {
		t.Errorf("old Now should be archived:\n%s", raw)
	}
	if !strings.Contains(raw, "rewrites") {
		t.Errorf("Out of scope should be preserved:\n%s", raw)
	}
}

func TestAdvanceRoadmap(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".mago"), 0o755)
	c := &Company{Dir: dir, Name: "co"}

	os.WriteFile(c.roadmapFile(), []byte(
		"# co — ROADMAP\n\n> intro\n\n## Now\nShip caps.\n\n## Next\nClose the loop.\n\n## Later\nMulti-agent.\n\n## Out of scope\nrewrites\n"), 0o644)

	if !c.advanceRoadmap() {
		t.Fatal("advanceRoadmap should succeed when Next is set")
	}
	if got := c.roadmapNow(); got != "Close the loop." {
		t.Errorf("Now should be the old Next, got %q", got)
	}
	if got := c.roadmapNext(); got != "Multi-agent." {
		t.Errorf("Next should be the old Later, got %q", got)
	}
	raw := c.roadmapRaw()
	if !strings.Contains(raw, "## Done") || !strings.Contains(raw, "Ship caps.") {
		t.Errorf("old Now should be archived under ## Done:\n%s", raw)
	}
	if !strings.Contains(raw, "rewrites") || !strings.Contains(raw, "> intro") {
		t.Errorf("unmanaged sections/preamble must be preserved:\n%s", raw)
	}

	// No Next to promote -> should not advance.
	os.WriteFile(c.roadmapFile(), []byte("## Now\nA.\n\n## Next\n(none yet)\n"), 0o644)
	if c.advanceRoadmap() {
		t.Error("should not advance when Next is a placeholder")
	}

	// Placeholder Now with a real Next -> should not advance (no real focus to archive).
	os.WriteFile(c.roadmapFile(), []byte("## Now\n(placeholder)\n\n## Next\nB.\n"), 0o644)
	if c.advanceRoadmap() {
		t.Error("should not advance when Now is a placeholder")
	}
	if got := c.roadmapNow(); got != "(placeholder)" {
		t.Errorf("Now should remain the placeholder, got %q", got)
	}
	if got := c.roadmapNext(); got != "B." {
		t.Errorf("Next should remain unchanged, got %q", got)
	}
}
