package main

import (
	"strings"
	"testing"
)

// TestCmdMode_BareCFlag verifies cmdMode returns an actionable error when a bare
// -C is given without a directory value, rather than falling through to a confusing
// loadCompany error.
func TestCmdMode_BareCFlag(t *testing.T) {
	err := cmdMode([]string{"-C"})
	if err == nil {
		t.Fatal("expected error for bare -C")
	}
	if !strings.Contains(err.Error(), "flag -C needs a directory") {
		t.Errorf("error = %q, want flag -C message", err.Error())
	}
}

// TestCmdMode_LoadCompanyError verifies cmdMode returns a clear error when the
// target directory is not a mago company.
func TestCmdMode_LoadCompanyError(t *testing.T) {
	t.Setenv("MAGO_GH_REPO", "")
	err := cmdMode([]string{"-C", t.TempDir(), "show"})
	if err == nil {
		t.Fatal("expected error for non-company directory")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error = %q, want 'not a mago company'", err.Error())
	}
}
