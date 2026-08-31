package main

import (
	"os"
	"testing"
)

func TestStateSyncOptIn(t *testing.T) {
	c := &Company{ghRepo: "owner/repo"}
	os.Unsetenv("MAGO_STATE_SYNC")
	if c.stateSyncEnabled() {
		t.Error("default must be OFF — never push state into a project repo unprompted")
	}
	t.Setenv("MAGO_STATE_SYNC", "1")
	if !c.stateSyncEnabled() {
		t.Error("MAGO_STATE_SYNC=1 should enable repo state-sync")
	}
	t.Setenv("MAGO_STATE_SYNC", " 1 ")
	if !c.stateSyncEnabled() {
		t.Error("MAGO_STATE_SYNC with surrounding whitespace should still enable sync")
	}
	// No repo -> never syncs, even opted in.
	if (&Company{ghRepo: ""}).stateSyncEnabled() {
		t.Error("no ghRepo -> no sync")
	}
}
