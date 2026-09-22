package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

	// A directory is not a readable file, so hashing it must return empty.
	if got := fileSHA12(dir); got != "" {
		t.Errorf("directory SHA should be empty, got %q", got)
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

// TestDownloadFile_DestOpenError verifies that an unwritable destination path
// (a parent directory that does not exist) is surfaced as an error after a
// successful 200 fetch — the download itself is fine, the local write is not.
func TestDownloadFile_DestOpenError(t *testing.T) {
	dir := t.TempDir()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("payload"))
	}))
	defer ts.Close()

	dst := filepath.Join(dir, "no-such-dir", "mago.new")
	if err := downloadFile(ts.URL, dst); err == nil {
		t.Fatal("downloadFile should error when the destination cannot be created")
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

// TestSwapIn verifies the spec §3 step-7 sequence: the current install moves to
// <target>.bak, the staged file lands in place, and the .bak is NOT deleted on
// success — it's the operator's manual rollback.
func TestSwapIn(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "mago")
	tmp := filepath.Join(dir, "mago.new.1")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	bak, err := swapIn(tmp, target)
	if err != nil {
		t.Fatalf("swapIn: %v", err)
	}
	if bak != target+".bak" {
		t.Errorf("bak = %q, want %q", bak, target+".bak")
	}
	if b, _ := os.ReadFile(target); string(b) != "new-binary" {
		t.Errorf("target = %q, want new-binary", b)
	}
	if b, _ := os.ReadFile(bak); string(b) != "old-binary" {
		t.Errorf(".bak = %q, want old-binary (kept for rollback)", b)
	}
}

// TestSwapIn_RestoresOnFailure verifies the rollback guarantee: when the staged file
// can't move into place, the .bak is restored so the tool is never left broken.
func TestSwapIn_RestoresOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "mago")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A staged path that does not exist forces the second rename to fail.
	_, err := swapIn(filepath.Join(dir, "missing-tmp"), target)
	if err == nil {
		t.Fatal("swapIn should fail when the staged file is missing")
	}
	if b, _ := os.ReadFile(target); string(b) != "old-binary" {
		t.Errorf("target = %q after failed swap, want old-binary (restored)", b)
	}
	if _, err := os.Stat(target + ".bak"); err == nil {
		t.Error(".bak should be moved back into place after a failed swap")
	}
}

// TestSwapIn_BackupFailure verifies that when the backup rename itself cannot run,
// the target is left untouched and the error is reported.
func TestSwapIn_BackupFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "no-such-dir", "mago") // parent missing: rename fails at once
	tmp := filepath.Join(dir, "mago.new.1")
	if err := os.WriteFile(tmp, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := swapIn(tmp, target); err == nil {
		t.Fatal("swapIn should fail when the backup rename cannot run")
	}
}

// TestUpdateLock verifies the cross-process guard: a held flock makes a second
// update report busy, and releasing it lets the next attempt through.
func TestUpdateLock(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "mago")

	release, err := updateLock(target)
	if err != nil {
		t.Fatalf("first updateLock: %v", err)
	}
	if _, err := updateLock(target); !errors.Is(err, errUpdateBusy) {
		t.Errorf("second updateLock err = %v, want errUpdateBusy", err)
	}
	release()
	if _, err := updateLock(target); err != nil {
		t.Errorf("updateLock after release: %v", err)
	}
}

// TestIsPermErr verifies permission failures are recognized (wrapped or bare)
// and other errors are not — this distinction is what stops the retry loop.
func TestIsPermErr(t *testing.T) {
	pe := &os.PathError{Op: "open", Path: "/usr/local/bin/mago.new.1", Err: syscall.EACCES}
	if !isPermErr(pe) {
		t.Error("EACCES PathError should be a permission error")
	}
	if !isPermErr(fmt.Errorf("swap: %w", pe)) {
		t.Error("wrapped EACCES should be a permission error")
	}
	if isPermErr(errors.New("HTTP 500")) {
		t.Error("generic error must not be a permission error")
	}
	if isPermErr(errUpdateBusy) {
		t.Error("errUpdateBusy must not be a permission error")
	}
}

// TestStageUpdate exercises the stage half of an update end-to-end: download,
// sha256[:12] verify, full-sha256 verify when advertised, chmod, probe — against a
// real temp target so no test-binary tricks are needed.
func TestStageUpdate(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("#!/bin/sh\necho 0.0.2-poc\n")
	fixture := filepath.Join(dir, "payload")
	if err := os.WriteFile(fixture, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	sha12, shaFull := fileSHA12(fixture), fileSHA256(fixture)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	defer ts.Close()

	target := filepath.Join(dir, "mago")

	// Happy path: hash + full-sha match + probe pass -> staged temp returned.
	tmp, err := stageUpdate(target, ts.URL, sha12, shaFull)
	if err != nil {
		t.Fatalf("stageUpdate: %v", err)
	}
	defer os.Remove(tmp)
	if fi, err := os.Stat(tmp); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("staged file missing or not executable: %v", err)
	}

	// A wrong full sha256 is rejected even when the 12-char version matches.
	if _, err := stageUpdate(target, ts.URL, sha12, strings.Repeat("0", 64)); err == nil {
		t.Error("full-sha256 mismatch should fail")
	}
	if _, err := os.Stat(fmt.Sprintf("%s.new.%d", target, os.Getpid())); err == nil {
		t.Error("stageUpdate left a temp file behind after rejection")
	}
}

// TestMaybeSelfUpdate_PermDeniedIsTerminal verifies the fix for the 163-failures-per-day
// loop: once a self-update hits EACCES, the worker reports it once, latches the denial,
// and only nudges thereafter — it never retries an operation that can't succeed until
// the filesystem changes.
func TestMaybeSelfUpdate_PermDeniedIsTerminal(t *testing.T) {
	lastNudgeVer = ""
	updateDenied = false
	updating = 0
	t.Setenv("MAGO_WORKER_ID", "test-worker")
	t.Setenv("MAGO_UPDATE", "auto")

	calls := 0
	orig := selfUpdateFn
	selfUpdateFn = func(string) (string, error) {
		calls++
		return "", &os.PathError{Op: "open", Path: "/usr/local/bin/mago.new.1", Err: syscall.EACCES}
	}
	defer func() { selfUpdateFn = orig; updateDenied = false; lastNudgeVer = "" }()

	w := &eventWorker{comp: &Company{Dir: t.TempDir()}}
	w.maybeSelfUpdate("new-ver")
	w.maybeSelfUpdate("new-ver") // denied: must NOT attempt again
	w.maybeSelfUpdate("new-ver")

	if calls != 1 {
		t.Errorf("selfUpdateFn called %d times, want 1 — perm-denied must be terminal", calls)
	}
	if !updateDenied {
		t.Error("updateDenied should latch after a permission failure")
	}
	if lastNudgeVer != "new-ver" {
		t.Errorf("lastNudgeVer = %q, want new-ver (fell back to the passive nudge)", lastNudgeVer)
	}
}

// TestMaybeSelfUpdate_TransientErrorRetries verifies that a non-permission failure
// (e.g. the platform mid-deploy) is NOT terminal: the next ping tries again.
func TestMaybeSelfUpdate_TransientErrorRetries(t *testing.T) {
	lastNudgeVer = ""
	updateDenied = false
	updating = 0
	t.Setenv("MAGO_WORKER_ID", "test-worker")
	t.Setenv("MAGO_UPDATE", "auto")

	calls := 0
	orig := selfUpdateFn
	selfUpdateFn = func(string) (string, error) {
		calls++
		return "", errors.New("HTTP 502")
	}
	defer func() { selfUpdateFn = orig; updateDenied = false; lastNudgeVer = "" }()

	w := &eventWorker{comp: &Company{Dir: t.TempDir()}}
	w.maybeSelfUpdate("new-ver")
	w.maybeSelfUpdate("new-ver")

	if calls != 2 {
		t.Errorf("selfUpdateFn called %d times, want 2 — transient failures retry", calls)
	}
	if updateDenied {
		t.Error("a non-permission error must not latch updateDenied")
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

// TestMaybeSelfUpdate_AlreadyInFlight verifies that a second concurrent self-update
// call bails out at the CAS guard instead of spawning a duplicate download/re-exec.
func TestMaybeSelfUpdate_AlreadyInFlight(t *testing.T) {
	lastNudgeVer = ""
	updating = 1
	t.Setenv("MAGO_WORKER_ID", "test-worker")
	t.Setenv("MAGO_UPDATE", "auto")
	defer func() { updating = 0 }()

	w := &eventWorker{comp: &Company{Dir: t.TempDir()}}
	w.maybeSelfUpdate("new-ver")

	if lastNudgeVer != "" {
		t.Errorf("in-flight auto mode should not nudge; lastNudgeVer = %q", lastNudgeVer)
	}
	if updating != 1 {
		t.Errorf("updating = %d, want 1 (no self-update attempt started)", updating)
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

// TestSelfUpdate_DownloadErrorCleansStaleTemp verifies that selfUpdate removes a
// pre-existing per-PID temp file when a download fails, so a previous crash or
// close-error cannot leave a stray .new.<pid> behind.
func TestSelfUpdate_DownloadErrorCleansStaleTemp(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	t.Setenv("MAGO_PLATFORM_URL", ts.URL)

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	tmp := fmt.Sprintf("%s.new.%d", exe, os.Getpid())
	if err := os.WriteFile(tmp, []byte("stale-download"), 0o644); err != nil {
		t.Fatalf("write stale temp: %v", err)
	}
	defer os.Remove(tmp)

	_, err = selfUpdate("any-hash")
	if err == nil {
		t.Fatal("selfUpdate with 404 download should fail")
	}

	if _, err := os.Stat(tmp); err == nil {
		t.Errorf("selfUpdate left stale temp file %q after download error", tmp)
	}
}
