package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	return key
}

func pemEncodePKCS1(key *rsa.PrivateKey) string {
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
}

func pemEncodePKCS8(key any) string {
	b, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: b,
	}))
}

func TestAppPrivateKey_Unset(t *testing.T) {
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	if _, err := appPrivateKey(); err == nil {
		t.Fatal("expected error when GITHUB_APP_PRIVATE_KEY is unset")
	}
}

func TestAppPrivateKey_MissingFile(t *testing.T) {
	t.Setenv("GITHUB_APP_PRIVATE_KEY", filepath.Join(t.TempDir(), "missing.pem"))
	_, err := appPrivateKey()
	if err == nil || !strings.Contains(err.Error(), "read app key") {
		t.Fatalf("expected a read error, got: %v", err)
	}
}

func TestAppPrivateKey_InlinePKCS1(t *testing.T) {
	key := testRSAKey(t)
	t.Setenv("GITHUB_APP_PRIVATE_KEY", pemEncodePKCS1(key))

	got, err := appPrivateKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || !got.PublicKey.Equal(&key.PublicKey) {
		t.Fatal("returned key does not match the input key")
	}
}

func TestAppPrivateKey_InlinePKCS8(t *testing.T) {
	key := testRSAKey(t)
	t.Setenv("GITHUB_APP_PRIVATE_KEY", pemEncodePKCS8(key))

	got, err := appPrivateKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || !got.PublicKey.Equal(&key.PublicKey) {
		t.Fatal("returned key does not match the input key")
	}
}

func TestAppPrivateKey_FromFile(t *testing.T) {
	key := testRSAKey(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "app.pem")
	if err := os.WriteFile(path, []byte(pemEncodePKCS1(key)), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	t.Setenv("GITHUB_APP_PRIVATE_KEY", path)
	got, err := appPrivateKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || !got.PublicKey.Equal(&key.PublicKey) {
		t.Fatal("returned key does not match the input key")
	}
}

func TestAppPrivateKey_NonRSA(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	t.Setenv("GITHUB_APP_PRIVATE_KEY", pemEncodePKCS8(ecKey))

	_, err = appPrivateKey()
	if err == nil || !strings.Contains(err.Error(), "not RSA") {
		t.Fatalf("expected 'not RSA' error, got: %v", err)
	}
}

func TestAppPrivateKey_BadPEM(t *testing.T) {
	// A string that looks like PEM but has no valid block should trigger the no-PEM-block error.
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "-----BEGIN PRIVATE KEY-----")
	_, err := appPrivateKey()
	if err == nil || !strings.Contains(err.Error(), "no PEM block") {
		t.Fatalf("expected 'no PEM block' error, got: %v", err)
	}
}

func TestAppPrivateKey_TrimmedWhitespace(t *testing.T) {
	key := testRSAKey(t)
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "  "+pemEncodePKCS1(key)+"\n\t ")

	got, err := appPrivateKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil || !got.PublicKey.Equal(&key.PublicKey) {
		t.Fatal("whitespace should be trimmed before parsing")
	}
}

func TestAppJWT_MissingAppID(t *testing.T) {
	key := testRSAKey(t)
	t.Setenv("GITHUB_APP_PRIVATE_KEY", pemEncodePKCS1(key))
	t.Setenv("GITHUB_APP_ID", "")

	_, err := appJWT()
	if err == nil || !strings.Contains(err.Error(), "GITHUB_APP_ID unset") {
		t.Fatalf("expected GITHUB_APP_ID unset error, got: %v", err)
	}
}

func TestAppJWT_OK(t *testing.T) {
	key := testRSAKey(t)
	t.Setenv("GITHUB_APP_PRIVATE_KEY", pemEncodePKCS1(key))
	t.Setenv("GITHUB_APP_ID", "123456")

	tok, err := appJWT()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(tok, "eyJ") {
		t.Fatalf("expected JWT prefix, got: %q", tok)
	}

	// Verify the signature with the public key.
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 JWT parts, got %d", len(parts))
	}
	h := crypto.SHA256.New()
	h.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := base64urlDecode(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, h.Sum(nil), sig); err != nil {
		t.Fatalf("JWT signature did not verify: %v", err)
	}
}

func base64urlDecode(s string) ([]byte, error) {
	if l := len(s) % 4; l > 0 {
		s += strings.Repeat("=", 4-l)
	}
	return base64.URLEncoding.DecodeString(s)
}
