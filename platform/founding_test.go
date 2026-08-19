package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFoundingBanner(t *testing.T) {
	full := foundingBanner(0)
	if !strings.Contains(full, "founding cohort is full") {
		t.Errorf("full banner missing cohort message: %q", full)
	}

	left := foundingBanner(3)
	if !strings.Contains(left, "3 of 10 slots left") {
		t.Errorf("banner should show remaining slots, got: %q", left)
	}
	if !strings.Contains(left, "free during beta") {
		t.Errorf("banner should mention free beta, got: %q", left)
	}
}

func TestIsInternalEmail(t *testing.T) {
	internal := []string{"founder@mago.test", "x@dogfood.test", "stripe-verify-1@example.com", "mago-dogfood@intrane.fr", "A@INTRANE.FR"}
	for _, e := range internal {
		if !isInternalEmail(e) {
			t.Errorf("%q should be internal", e)
		}
	}
	for _, e := range []string{"jane@acme.com", "ops@startup.io", "dev@gmail.com"} {
		if isInternalEmail(e) {
			t.Errorf("%q should be external (a real operator)", e)
		}
	}
}

func TestFoundingEntitlementAndSlots(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if st.FoundingSlotsLeft() != foundingCap {
		t.Fatalf("fresh store should have %d slots, got %d", foundingCap, st.FoundingSlotsLeft())
	}
	// Claim 3 founding slots.
	st.db.Exec("INSERT INTO users (email, password_hash, plan, created_at) VALUES ('a@x.com','h','founding',0),('b@x.com','h','founding',0),('c@x.com','h','founding',0)")
	if got := st.FoundingCount(); got != 3 {
		t.Errorf("FoundingCount=%d, want 3", got)
	}
	if got := st.FoundingSlotsLeft(); got != foundingCap-3 {
		t.Errorf("SlotsLeft=%d, want %d", got, foundingCap-3)
	}

	// founding is entitled with no expiry; expired trial is not.
	if !(&User{Plan: "founding"}).entitled() {
		t.Error("founding must be entitled")
	}
	if (&User{Plan: "trial", TrialEnds: 1}).entitled() {
		t.Error("expired trial must not be entitled")
	}
}
