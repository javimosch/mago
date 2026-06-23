package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// selfupdate.go lets a worker update its own binary with no ssh and no manual reship. The platform
// advertises the version (sha256[:12]) of the CLI binary it serves for the worker's os/arch on the
// relay ready/ping frames; the worker compares it to its own running binary and, when they differ:
//   - update=auto: downloads /dl/mago?os=&arch=, verifies the hash, atomically swaps its binary, and
//     re-execs itself (syscall.Exec keeps the same PID, so a --daemon supervisor neither double-spawns
//     nor needs to restart — the running process simply becomes the new binary).
//   - update=manual (default): logs a one-time nudge so the operator can flip `update=auto`.
// The version is a content hash, so no version-bump discipline is needed: identical bytes => no update.

// selfVersion is sha256[:12] of this running executable, computed once.
var (
	selfVerOnce sync.Once
	selfVer     string
)

func selfVersion() string {
	selfVerOnce.Do(func() {
		if exe, err := os.Executable(); err == nil {
			selfVer = fileSHA12(exe)
		}
	})
	return selfVer
}

// fileSHA12 returns the first 12 hex chars of a file's sha256, or "" on error.
func fileSHA12(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

var (
	updating     int32  // CAS guard: one self-update attempt at a time
	lastNudgeVer string // throttle the manual-mode nudge to once per advertised version
	nudgeMu      sync.Mutex
)

// maybeSelfUpdate is called with the version the platform advertises for this worker's os/arch.
// It no-ops unless that differs from the running binary; then it either self-updates (auto) or
// nudges (manual). Safe to call on every ping — the version compare makes the steady state free.
func (w *eventWorker) maybeSelfUpdate(latest string) {
	if latest == "" || latest == selfVersion() {
		return
	}
	if w.comp.modeUpdate() != "auto" {
		nudgeMu.Lock()
		if lastNudgeVer != latest {
			lastNudgeVer = latest
			fmt.Fprintf(os.Stderr, "[update] a newer mago (%s) is available; running %s. "+
				"Enable self-update: `mago worker mode update=auto --worker %s`\n", latest, selfVersion(), workerID())
		}
		nudgeMu.Unlock()
		return
	}
	if !atomic.CompareAndSwapInt32(&updating, 0, 1) {
		return // an attempt is already in flight
	}
	defer atomic.StoreInt32(&updating, 0)
	exe, err := selfUpdate(latest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[update] failed (staying on %s): %v\n", selfVersion(), err)
		return
	}
	fmt.Fprintf(os.Stderr, "[update] %s -> %s; re-execing\n", selfVersion(), latest)
	// syscall.Exec replaces this process image in place (same PID). A supervisor's Wait() never
	// returns, so it won't double-spawn; a foreground worker simply restarts on the new binary.
	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "[update] re-exec failed: %v (next restart picks up the new binary)\n", err)
	}
}

// selfUpdate downloads the platform's binary for this os/arch, verifies it hashes to the advertised
// version (guards a mid-deploy race), and atomically swaps it over the running executable. It returns
// the executable path (captured before the swap) for the caller to re-exec.
func selfUpdate(latest string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	cfg := loadConfig()
	url := strings.TrimRight(cfg.PlatformURL, "/") + "/dl/mago?os=" + runtime.GOOS + "&arch=" + runtime.GOARCH
	// Per-PID temp so two workers sharing one binary path (several companies on a box) don't clobber
	// each other's download; the rename is atomic and both write identical bytes, so last-wins is safe.
	tmp := fmt.Sprintf("%s.new.%d", exe, os.Getpid()) // same dir => same fs => atomic rename
	if err := downloadFile(url, tmp); err != nil {
		return "", err
	}
	if got := fileSHA12(tmp); got != latest {
		os.Remove(tmp)
		return "", fmt.Errorf("downloaded %s != advertised %s (deploy in progress?)", got, latest)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return exe, nil
}

// downloadFile streams url to path (truncating any existing file).
func downloadFile(url, path string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}
