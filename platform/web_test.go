package main

import (
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
