package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestB64(t *testing.T) {
	got := b64(`{"alg":"HS256","typ":"JWT"}`)
	want := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	if got != want {
		t.Errorf("b64() = %q, want %q", got, want)
	}
}

func TestSign(t *testing.T) {
	msg := "header.payload"
	secret := "shh"
	got := sign(msg, secret)
	want := base64.RawURLEncoding.EncodeToString(hmacSHA256(msg, secret))
	if got != want {
		t.Errorf("sign(%q, %q) = %q, want %q", msg, secret, got, want)
	}
}

func hmacSHA256(msg, secret string) []byte {
	// duplicate the private sign logic for an independent reference
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return m.Sum(nil)
}

func TestJWTSignVerify(t *testing.T) {
	secret := "test-secret"
	uid := int64(42)
	email := "dev@example.com"

	token := jwtSign(secret, uid, email)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwtSign produced %d parts, want 3", len(parts))
	}

	gotUID, ok := jwtVerify(secret, token)
	if !ok {
		t.Fatalf("jwtVerify rejected a freshly signed token")
	}
	if gotUID != uid {
		t.Errorf("jwtVerify uid = %d, want %d", gotUID, uid)
	}

	// tampered signature should fail
	if _, ok := jwtVerify(secret, token+"x"); ok {
		t.Error("jwtVerify accepted a tampered token")
	}
	if _, ok := jwtVerify("wrong-secret", token); ok {
		t.Error("jwtVerify accepted a token with the wrong secret")
	}
}

func TestGenLicense(t *testing.T) {
	k := genLicense()
	if !strings.HasPrefix(k, "mago_") {
		t.Errorf("genLicense() = %q, want 'mago_' prefix", k)
	}
	hexPart := strings.TrimPrefix(k, "mago_")
	if len(hexPart) != 48 {
		t.Errorf("genLicense() hex part length = %d, want 48", len(hexPart))
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		t.Errorf("genLicense() hex part not valid hex: %v", err)
	}
}
