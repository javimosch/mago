package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeBinary(t *testing.T) {
	dir := t.TempDir()

	// A runnable program that prints output for `version` passes the probe.
	good := filepath.Join(dir, "good")
	if err := os.WriteFile(good, []byte("#!/bin/sh\necho 0.0.1-poc\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := probeBinary(good); err != nil {
		t.Errorf("runnable binary should pass the probe, got: %v", err)
	}

	// A truncated/corrupt binary (the rbm21 brick scenario) must be rejected — it won't exec.
	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, []byte("\x7fELF\x00truncated-garbage-not-a-real-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := probeBinary(bad); err == nil {
		t.Error("corrupt binary must be rejected by the probe")
	}

	// A binary that runs but prints nothing is rejected (no usable version output).
	silent := filepath.Join(dir, "silent")
	os.WriteFile(silent, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	if err := probeBinary(silent); err == nil {
		t.Error("empty-output binary must be rejected")
	}

	// A missing path is rejected.
	if err := probeBinary(filepath.Join(dir, "nope")); err == nil {
		t.Error("missing binary must be rejected")
	}
}

// TestFileSHA12 verifies the helper returns the first 12 hex chars of a file's
// sha256, and an empty string when the file is missing.
func TestFileSHA12(t *testing.T) {
	dir := t.TempDir()

	// Known SHA-256 for "hello" -> 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := fileSHA12(path); got != "2cf24dba5fb0" {
		t.Errorf("fileSHA12(hello) = %q, want %q", got, "2cf24dba5fb0")
	}

	if got := fileSHA12(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("missing file SHA should be empty, got %q", got)
	}
}

// TestSelfVersion verifies the running executable hash is returned as a
// 12-character hex string and is cached across calls.
func TestSelfVersion(t *testing.T) {
	got := selfVersion()
	if len(got) != 12 {
		t.Fatalf("selfVersion() length = %d, want 12", len(got))
	}
	for i := 0; i < len(got); i++ {
		c := got[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("selfVersion() = %q, want lowercase hex", got)
		}
	}
	if got2 := selfVersion(); got2 != got {
		t.Fatalf("selfVersion() not cached: %q vs %q", got, got2)
	}
}

// TestDownloadFile verifies the helper streams a 200 response to disk and
// surfaces non-200 status codes as errors.
func TestDownloadFile(t *testing.T) {
	dir := t.TempDir()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("payload"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	dst := filepath.Join(dir, "ok")
	if err := downloadFile(ts.URL+"/ok", dst); err != nil {
		t.Fatalf("downloadFile(200) error: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("downloadFile did not create destination: %v", err)
	}
	if string(got) != "payload" {
		t.Errorf("downloaded content = %q, want %q", got, "payload")
	}

	if err := downloadFile(ts.URL+"/missing", filepath.Join(dir, "missing")); err == nil {
		t.Error("downloadFile(404) should return an error")
	}
}

// TestDownloadFile_ConnectionError verifies that a completely unreachable
// platform URL is reported as an error rather than being swallowed.
func TestDownloadFile_ConnectionError(t *testing.T) {
	dir := t.TempDir()
	if err := downloadFile("http://127.0.0.1:1/nope", filepath.Join(dir, "nope")); err == nil {
		t.Error("downloadFile should surface a connection error")
	}
}

// TestDownloadFile_TruncatedBody verifies that a server advertising a larger
// Content-Length than it actually sends is treated as a copy error and the
// partial temp file is removed so a broken download cannot be swapped in.
func TestDownloadFile_TruncatedBody(t *testing.T) {
	dir := t.TempDir()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("short"))
	}))
	defer ts.Close()

	dst := filepath.Join(dir, "truncated")
	if err := downloadFile(ts.URL, dst); err == nil {
		t.Fatal("downloadFile should error on truncated body")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Errorf("downloadFile left partial temp file %q after copy error", dst)
	}
}

// TestMaybeSelfUpdate verifies the nudge logic in manual mode: same or empty
// versions are ignored, a new version records the nudge, and repeated calls
// for the same version do not nudge again.
func TestMaybeSelfUpdate(t *testing.T) {
	lastNudgeVer = ""
	t.Setenv("MAGO_WORKER_ID", "test-worker")
	t.Setenv("MAGO_UPDATE", "")

	w := &eventWorker{comp: &Company{Dir: t.TempDir()}}

	// Same as running binary -> no nudge.
	w.maybeSelfUpdate(selfVersion())
	if lastNudgeVer != "" {
		t.Errorf("same version should not nudge, lastNudgeVer=%q", lastNudgeVer)
	}

	// Empty version -> no nudge.
	w.maybeSelfUpdate("")
	if lastNudgeVer != "" {
		t.Errorf("empty version should not nudge, lastNudgeVer=%q", lastNudgeVer)
	}

	// New version in manual mode -> nudge and record it.
	w.maybeSelfUpdate("new-ver")
	if lastNudgeVer != "new-ver" {
		t.Errorf("lastNudgeVer = %q, want new-ver", lastNudgeVer)
	}

	// Repeated same version -> no double nudge.
	w.maybeSelfUpdate("new-ver")
	if lastNudgeVer != "new-ver" {
		t.Errorf("lastNudgeVer changed unexpectedly to %q", lastNudgeVer)
	}
}

// TestMaybeSelfUpdate_AutoModeSurfacesError verifies that when update=auto is set,
// a failed self-update attempt (here, an unreachable platform) is reported but does
// not emit a manual-mode nudge or corrupt the nudge state.
func TestMaybeSelfUpdate_AutoModeSurfacesError(t *testing.T) {
	lastNudgeVer = ""
	updating = 0
	t.Setenv("MAGO_WORKER_ID", "test-worker")
	t.Setenv("MAGO_UPDATE", "auto")
	t.Setenv("MAGO_PLATFORM_URL", "http://127.0.0.1:1")

	w := &eventWorker{comp: &Company{Dir: t.TempDir()}}
	w.maybeSelfUpdate("new-ver")

	if lastNudgeVer != "" {
		t.Errorf("auto mode should not nudge; lastNudgeVer = %q", lastNudgeVer)
	}
}

// TestSelfUpdate_HashMismatch verifies that selfUpdate rejects a downloaded binary whose
// sha256[:12] does not match the advertised version, and that the temporary download is
// cleaned up so a mid-deploy race cannot leave a stray .new.<pid> file behind.
func TestSelfUpdate_HashMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dl/mago" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("not-the-advertised-binary"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	t.Setenv("MAGO_PLATFORM_URL", ts.URL)

	_, err := selfUpdate("wrong-hash")
	if err == nil {
		t.Fatal("selfUpdate with hash mismatch should fail")
	}
	if !strings.Contains(err.Error(), "advertised") {
		t.Errorf("error should mention advertised version, got: %v", err)
	}

	// The per-PID temp file must be removed on mismatch.
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	tmp := fmt.Sprintf("%s.new.%d", exe, os.Getpid())
	if _, err := os.Stat(tmp); err == nil {
		t.Errorf("selfUpdate left stale temp file %q after hash mismatch", tmp)
	}
}

// TestSelfUpdate_ProbeRejected verifies that a downloaded binary passing the hash check
// but failing the runtime `mago version` probe is rejected and the temp file is cleaned up.
// This is the rbm21 brick regression: a self-consistent partial publish can hash-match
// but not actually execute, so the probe must catch it before the live binary is replaced.
func TestSelfUpdate_ProbeRejected(t *testing.T) {
	dir := t.TempDir()

	// A file that runs and exits cleanly but prints nothing: the probe rejects empty output.
	script := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(filepath.Join(dir, "payload"), script, 0o644); err != nil {
		t.Fatal(err)
	}
	wantHash := fileSHA12(filepath.Join(dir, "payload"))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dl/mago" {
			w.WriteHeader(http.StatusOK)
			w.Write(script)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	t.Setenv("MAGO_PLATFORM_URL", ts.URL)

	_, err := selfUpdate(wantHash)
	if err == nil {
		t.Fatal("selfUpdate with a failing probe should fail")
	}
	if !strings.Contains(err.Error(), "downloaded binary rejected") {
		t.Errorf("error should mention probe rejection, got: %v", err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	tmp := fmt.Sprintf("%s.new.%d", exe, os.Getpid())
	if _, err := os.Stat(tmp); err == nil {
		t.Errorf("selfUpdate left stale temp file %q after probe rejection", tmp)
	}
}

// TestSelfUpdate_DownloadError verifies that selfUpdate reports a failed download
// cleanly and does not leave a temp file when the platform returns a non-200 status.
func TestSelfUpdate_DownloadError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	t.Setenv("MAGO_PLATFORM_URL", ts.URL)

	_, err := selfUpdate("any-hash")
	if err == nil {
		t.Fatal("selfUpdate with 404 download should fail")
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("error should mention HTTP 404, got: %v", err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	tmp := fmt.Sprintf("%s.new.%d", exe, os.Getpid())
	if _, err := os.Stat(tmp); err == nil {
		t.Errorf("selfUpdate left stale temp file %q after download error", tmp)
	}
}
