package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestValidSignature(t *testing.T) {
	secret := "shhh"
	body := []byte("payload")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !validSignature(secret, want, body) {
		t.Errorf("validSignature(%q, %q, %q) = false, want true", secret, want, body)
	}

	// Wrong secret produces a different mac.
	if validSignature("other", want, body) {
		t.Errorf("validSignature with wrong secret should fail")
	}

	// Missing or malformed prefix is rejected.
	if validSignature(secret, hex.EncodeToString(mac.Sum(nil)), body) {
		t.Errorf("validSignature without sha256= prefix should fail")
	}
	if validSignature(secret, "sha1=deadbeef", body) {
		t.Errorf("validSignature with sha1 prefix should fail")
	}
	if validSignature(secret, "", body) {
		t.Errorf("validSignature with empty signature should fail")
	}
}

func TestUntilDuration(t *testing.T) {
	// Valid HH:MM → a duration in (0, 24h].
	d, err := untilDuration("09:00")
	if err != nil {
		t.Fatalf("untilDuration(09:00): %v", err)
	}
	if d <= 0 || d > 24*time.Hour {
		t.Errorf("duration %s out of (0,24h]", d)
	}
	// A time one minute from now is ~today (well under 24h), not pushed to tomorrow.
	soon := time.Now().Add(time.Minute).Format("15:04")
	if d, _ := untilDuration(soon); d > 23*time.Hour {
		t.Errorf("near-future %s should be ~today, got %s", soon, d)
	}
	// Invalid input errors.
	for _, bad := range []string{"25:00", "9am", "", "09:99"} {
		if _, err := untilDuration(bad); err == nil {
			t.Errorf("untilDuration(%q) should error", bad)
		}
	}
}
