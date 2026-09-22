package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"syscall"
	"testing"
)

// TestFetchServerVersion verifies the §2 client: a 200 yields the advertised version,
// download path, and full sha256; error bodies and malformed payloads are surfaced.
func TestFetchServerVersion(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"version":"abc123def456","download":"/dl/mago?os=%s&arch=%s","sha256":"%s"}`,
				r.URL.Query().Get("os"), r.URL.Query().Get("arch"), "deadbeef")
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()
	t.Setenv("MAGO_PLATFORM_URL", ts.URL)

	sv, err := fetchServerVersion()
	if err != nil {
		t.Fatalf("fetchServerVersion: %v", err)
	}
	if sv.Version != "abc123def456" || sv.SHA256 != "deadbeef" {
		t.Errorf("sv = %+v, want version abc123def456 + sha256 deadbeef", sv)
	}
	wantDL := "/dl/mago?os=" + runtime.GOOS + "&arch=" + runtime.GOARCH
	if sv.Download != wantDL {
		t.Errorf("download = %q, want %q", sv.Download, wantDL)
	}
}

// TestFetchServerVersion_Errors verifies non-200 and malformed responses come back
// as errors (the command must not update against a missing endpoint).
func TestFetchServerVersion_Errors(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"ok":false,"error":"no cli binary published"}`)
	}))
	defer ts.Close()
	t.Setenv("MAGO_PLATFORM_URL", ts.URL)

	if _, err := fetchServerVersion(); err == nil {
		t.Fatal("404 /version should error")
	}

	t.Setenv("MAGO_PLATFORM_URL", "http://127.0.0.1:1")
	if _, err := fetchServerVersion(); err == nil {
		t.Fatal("unreachable platform should error")
	}
}

// TestServerVersionURL verifies download-URL resolution: relative paths join the
// platform base, absolute URLs pass through, and an empty field falls back to the
// well-known /dl/mago route for this os/arch.
func TestServerVersionURL(t *testing.T) {
	base := "https://mago.example/"
	if got := (&serverVersion{Download: "/dl/mago?os=linux&arch=amd64"}).url(base); got != "https://mago.example/dl/mago?os=linux&arch=amd64" {
		t.Errorf("relative download resolved to %q", got)
	}
	if got := (&serverVersion{Download: "https://cdn.example/mago"}).url(base); got != "https://cdn.example/mago" {
		t.Errorf("absolute download resolved to %q", got)
	}
	want := "https://mago.example/dl/mago?os=" + runtime.GOOS + "&arch=" + runtime.GOARCH
	if got := (&serverVersion{}).url(base); got != want {
		t.Errorf("empty download resolved to %q, want %q", got, want)
	}
}

// TestCmdUpdate_Check verifies the §3 --check contract: exit 5 when an update is
// available (a silent semantic exit, not an error message), 0 when up-to-date.
func TestCmdUpdate_Check(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"version":"ffffffffffff","download":"/dl/mago","sha256":""}`)
	}))
	defer ts.Close()
	t.Setenv("MAGO_PLATFORM_URL", ts.URL)

	err := cmdUpdate([]string{"--check"})
	var ce *cliErr
	if !errors.As(err, &ce) || ce.code != 5 {
		t.Fatalf("cmdUpdate --check with update available = %v, want cliErr code 5", err)
	}
}

// TestCmdUpdate_CheckUpToDate verifies --check exits cleanly when the advertised
// version matches the running binary.
func TestCmdUpdate_CheckUpToDate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":true,"version":%q,"download":"/dl/mago"}`, selfVersion())
	}))
	defer ts.Close()
	t.Setenv("MAGO_PLATFORM_URL", ts.URL)

	if err := cmdUpdate([]string{"--check"}); err != nil {
		t.Errorf("cmdUpdate --check up-to-date = %v, want nil", err)
	}
}

// TestCmdUpdate_BadFlag verifies unknown flags are a typed user error (80).
func TestCmdUpdate_BadFlag(t *testing.T) {
	var ce *cliErr
	if err := cmdUpdate([]string{"--bogus"}); !errors.As(err, &ce) || ce.code != 80 {
		t.Errorf("cmdUpdate --bogus = %v, want cliErr code 80", err)
	}
}

// TestCmdUpdate_ServerUnreachable verifies a failed /version fetch is the typed
// integration error (100).
func TestCmdUpdate_ServerUnreachable(t *testing.T) {
	t.Setenv("MAGO_PLATFORM_URL", "http://127.0.0.1:1")
	var ce *cliErr
	if err := cmdUpdate(nil); !errors.As(err, &ce) || ce.code != 100 {
		t.Errorf("cmdUpdate with dead platform = %v, want cliErr code 100", err)
	}
}

// TestCmdUpdate_Busy verifies that while another process holds the update lock,
// `mago update` aborts instead of racing the swap.
func TestCmdUpdate_Busy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"version":"ffffffffffff","download":"/dl/mago"}`)
	}))
	defer ts.Close()
	t.Setenv("MAGO_PLATFORM_URL", ts.URL)

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	lf, err := os.OpenFile(exe+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatalf("create lock file: %v", err)
	}
	defer os.Remove(exe + ".lock")
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("hold flock: %v", err)
	}

	var ce *cliErr
	if err := cmdUpdate(nil); !errors.As(err, &ce) || ce.code != 100 {
		t.Errorf("cmdUpdate while locked = %v, want cliErr code 100", err)
	}
}
