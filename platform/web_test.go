package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleDownloadUnsupported(t *testing.T) {
	s := &server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dl/mago?os=windows&arch=amd64", nil)

	s.handleDownload(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "unsupported platform") {
		t.Errorf("body = %q, want unsupported platform error", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestHandleDownloadSuccess(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "mago-linux-amd64")
	if err := os.WriteFile(binPath, []byte("fake binary bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGO_CLI_DIR", dir)

	s := &server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dl/mago?os=linux&arch=amd64", nil)

	s.handleDownload(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
	body := rec.Body.String()
	if body != "fake binary bytes" {
		t.Errorf("body = %q, want fake binary bytes", body)
	}
}

func TestHandleDownloadTrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "mago-linux-amd64")
	if err := os.WriteFile(binPath, []byte("fake binary bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &server{}

	// A whitespace-only MAGO_CLI_DIR falls through to the linux-amd64 legacy
	// fallback, and a padded MAGO_CLI_BINARY is trimmed before opening.
	t.Setenv("MAGO_CLI_DIR", "   ")
	t.Setenv("MAGO_CLI_BINARY", "  "+binPath+"  ")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dl/mago?os=linux&arch=amd64", nil)
	s.handleDownload(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "fake binary bytes" {
		t.Errorf("body = %q, want fake binary bytes", rec.Body.String())
	}

	// Whitespace-only values are treated as unset, so the handler reports not published.
	t.Setenv("MAGO_CLI_DIR", "   ")
	t.Setenv("MAGO_CLI_BINARY", "   ")

	rec = httptest.NewRecorder()
	s.handleDownload(rec, req)
	if rec.Code != 503 {
		t.Errorf("whitespace-only env status = %d, want 503", rec.Code)
	}
}

func TestHandleDownloadDefaultsToLinuxAmd64(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "mago-linux-amd64")
	if err := os.WriteFile(binPath, []byte("default binary bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGO_CLI_DIR", dir)

	s := &server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dl/mago", nil)

	s.handleDownload(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "default binary bytes" {
		t.Errorf("body = %q, want default binary bytes", rec.Body.String())
	}
}

func TestHandleDownloadMissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAGO_CLI_DIR", dir)

	s := &server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/dl/mago?os=linux&arch=amd64", nil)

	s.handleDownload(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "cli binary unavailable") {
		t.Errorf("body = %q, want 'cli binary unavailable'", body)
	}
}

// TestHandleVersion verifies the cli-update-spec §2 endpoint: a published binary
// yields its content-hash version, download path, and full sha256 — computed from
// the actual artifact on disk, not a maintained version string.
func TestHandleVersion(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("fake binary bytes")
	binPath := filepath.Join(dir, "mago-linux-amd64")
	if err := os.WriteFile(binPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGO_CLI_DIR", dir)
	t.Setenv("MAGO_CLI_BINARY", "")

	cliVerMu.Lock()
	cliVerCache = map[string]cliVerEntry{}
	cliVerMu.Unlock()

	s := &server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/version?os=linux&arch=amd64", nil)
	s.handleVersion(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	h := sha256.New()
	h.Write(payload)
	wantSum := hex.EncodeToString(h.Sum(nil))
	for _, frag := range []string{`"ok":true`, `"version":"` + wantSum[:12] + `"`, `"sha256":"` + wantSum + `"`, `"download":"/dl/mago?os=linux`} {
		if !strings.Contains(body, frag) {
			t.Errorf("body missing %s: %s", frag, body)
		}
	}
}

// TestHandleVersion_NotPublished verifies /version returns 404 (per §2) when no
// artifact exists for the platform — including an unsupported os/arch.
func TestHandleVersion_NotPublished(t *testing.T) {
	t.Setenv("MAGO_CLI_DIR", t.TempDir()) // dir exists but holds no binary
	t.Setenv("MAGO_CLI_BINARY", "")

	cliVerMu.Lock()
	cliVerCache = map[string]cliVerEntry{}
	cliVerMu.Unlock()

	s := &server{}

	rec := httptest.NewRecorder()
	s.handleVersion(rec, httptest.NewRequest("GET", "/version?os=linux&arch=amd64", nil))
	if rec.Code != 404 {
		t.Errorf("unpublished platform status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("404 body should carry an error message: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	s.handleVersion(rec, httptest.NewRequest("GET", "/version?os=windows&arch=amd64", nil))
	if rec.Code != 404 {
		t.Errorf("unsupported platform status = %d, want 404", rec.Code)
	}
}

// TestHandleVersion_LegacyFallback verifies that when MAGO_CLI_DIR is unset, the
// legacy single-file MAGO_CLI_BINARY still advertises a version for linux-amd64 —
// the same artifact /dl/mago would serve.
func TestHandleVersion_LegacyFallback(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "mago")
	if err := os.WriteFile(binPath, []byte("legacy binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAGO_CLI_DIR", "   ")
	t.Setenv("MAGO_CLI_BINARY", binPath)

	cliVerMu.Lock()
	cliVerCache = map[string]cliVerEntry{}
	cliVerMu.Unlock()

	s := &server{}
	rec := httptest.NewRecorder()
	s.handleVersion(rec, httptest.NewRequest("GET", "/version", nil)) // defaults linux/amd64
	if rec.Code != 200 {
		t.Fatalf("legacy fallback status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Errorf("body = %s", rec.Body.String())
	}

	// The fallback only applies to linux-amd64 — other platforms stay unpublished.
	rec = httptest.NewRecorder()
	s.handleVersion(rec, httptest.NewRequest("GET", "/version?os=darwin&arch=arm64", nil))
	if rec.Code != 404 {
		t.Errorf("darwin-arm64 without MAGO_CLI_DIR status = %d, want 404", rec.Code)
	}
}

func TestHandleInstall(t *testing.T) {
	s := &server{appURL: "http://localhost:9100"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/install.sh", nil)

	s.handleInstall(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/x-shellscript") {
		t.Errorf("Content-Type = %q, want text/x-shellscript", ct)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "#!/bin/sh") {
		t.Errorf("body should start with #!/bin/sh, got: %q", body)
	}
	if !strings.Contains(body, "http://localhost:9100") {
		t.Errorf("body should contain appURL, got: %q", body)
	}
}

func TestHandleLandingNotFound(t *testing.T) {
	s := &server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/missing", nil)

	s.handleLanding(rec, req)

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleLanding(t *testing.T) {
	st, err := openStore(filepath.Join(t.TempDir(), "landing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	s := &server{appURL: "http://localhost:9100", store: st}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	s.handleLanding(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "mago") {
		t.Errorf("body missing title, got: %q", body)
	}
	if !strings.Contains(body, "http://localhost:9100") {
		t.Errorf("body missing appURL, got: %q", body)
	}
}

func TestHandleOperators(t *testing.T) {
	s := &server{appURL: "http://localhost:9100"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/operators", nil)

	s.handleOperators(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Operator guide") {
		t.Errorf("body missing title, got: %q", body)
	}
	if !strings.Contains(body, "http://localhost:9100") {
		t.Errorf("body missing appURL, got: %q", body)
	}
}

func TestHandleLLMs(t *testing.T) {
	s := &server{appURL: "http://localhost:9100"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/llms.txt", nil)

	s.handleLLMs(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "## Prerequisites") {
		t.Errorf("body missing heading, got: %q", body)
	}
	if !strings.Contains(body, "http://localhost:9100") {
		t.Errorf("body missing appURL, got: %q", body)
	}
}
