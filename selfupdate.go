package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
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
//   - update=auto: downloads /dl/mago?os=&arch=, verifies the hash, smoke-tests the download,
//     atomically swaps its binary (the old one moves to <exe>.bak for manual rollback), and
//     re-execs itself (syscall.Exec keeps the same PID, so a --daemon supervisor neither
//     double-spawns nor needs to restart — the running process simply becomes the new binary).
//   - update=manual (default): logs a one-time nudge so the operator can run `mago update`.
// The version is a content hash, so no version-bump discipline is needed: identical bytes => no
// update, and converging on the published artifact is the intent even when that's a move BACK
// (the server is authoritative; there is no newer/older, only same/different).
//
// cli-update-spec framing: `mago update` (update.go) is the spec's §3 verb; update=auto is mago's
// opt-in extension beyond the spec — the §4 nudge itself never auto-updates, it only prints.

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

// fileSHA256 returns the full sha256 hex of a file, or "" on error.
func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// fileSHA12 returns the first 12 hex chars of a file's sha256, or "" on error.
func fileSHA12(path string) string {
	if sum := fileSHA256(path); len(sum) >= 12 {
		return sum[:12]
	}
	return ""
}

var (
	updating     int32  // CAS guard: one self-update attempt per process at a time
	lastNudgeVer string // throttle the nudge to once per advertised version
	updateDenied bool   // latched on EACCES: the swap can't succeed until the fs changes — nudge only
	nudgeMu      sync.Mutex
)

// selfUpdateFn is what maybeSelfUpdate calls to perform the swap; a var so tests can inject
// failures (a real attempt would overwrite the test binary itself).
var selfUpdateFn = selfUpdate

// errUpdateBusy marks a lost flock race: another mago process is already mid-swap on this binary.
var errUpdateBusy = errors.New("another update is already in progress")

// maybeSelfUpdate is called with the version the platform advertises for this worker's os/arch.
// It no-ops unless that differs from the running binary; then it either self-updates (auto) or
// nudges (manual). Safe to call on every ping — the version compare makes the steady state free.
func (w *eventWorker) maybeSelfUpdate(latest string) {
	if latest == "" || latest == selfVersion() {
		return
	}
	nudgeMu.Lock()
	denied := updateDenied
	nudgeMu.Unlock()
	// Once a permission failure has latched, retrying is the 163-line failure loop this fixed:
	// the outcome cannot change until the filesystem does, so degrade to the passive nudge.
	if denied || w.comp.modeUpdate() != "auto" {
		nudgeUpdate(latest, denied)
		return
	}
	if !atomic.CompareAndSwapInt32(&updating, 0, 1) {
		return // an attempt is already in flight
	}
	defer atomic.StoreInt32(&updating, 0)
	exe, err := selfUpdateFn(latest)
	if err != nil {
		if errors.Is(err, errUpdateBusy) {
			return // another worker sharing this binary path is mid-swap; next ping retries
		}
		if isPermErr(err) {
			// Terminal until the filesystem changes: report once (path + user), then nudge-only.
			nudgeMu.Lock()
			updateDenied = true
			nudgeMu.Unlock()
			fmt.Fprintf(os.Stderr, "[update] %s is not writable by %s — self-update disabled for this process; "+
				"install to a writable prefix (`mago install`) or fix directory permissions\n", exePath(), currentUser())
			nudgeUpdate(latest, true)
			return
		}
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

// nudgeUpdate is the passive nudge (cli-update-spec §4): stderr only, throttled to once per
// advertised version per process, and it NEVER auto-updates. denied=true when a prior attempt
// proved the binary's directory isn't writable by this user — then the fix is relocation, not
// flipping update=auto (which would just fail again).
func nudgeUpdate(latest string, denied bool) {
	nudgeMu.Lock()
	defer nudgeMu.Unlock()
	if lastNudgeVer == latest {
		return
	}
	lastNudgeVer = latest
	if denied {
		fmt.Fprintf(os.Stderr, "[update] a newer mago is available (%s → %s), but %s is not writable by %s. "+
			"Relocate it: `mago install` (→ ~/.local/bin) then run `mago update` from the new path.\n",
			selfVersion(), latest, exePath(), currentUser())
		return
	}
	fmt.Fprintf(os.Stderr, "[update] a newer mago is available (%s → %s). Run: mago update "+
		"— or go hands-free: `mago worker mode update=auto --worker %s`\n",
		selfVersion(), latest, workerID())
}

// selfUpdate swaps the running executable for the platform's published build of this os/arch.
// Returns the executable path (captured before the swap) for the caller to re-exec.
func selfUpdate(latest string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	cfg := loadConfig()
	url := strings.TrimRight(cfg.PlatformURL, "/") + "/dl/mago?os=" + runtime.GOOS + "&arch=" + runtime.GOARCH
	if _, err := runUpdate(exe, url, latest, ""); err != nil {
		return "", err
	}
	return exe, nil
}

// runUpdate performs the full swap under a cross-process lock: stage the download beside the
// target, then swap it in with a .bak rollback. Shared by the worker's auto mode and `mago update`.
func runUpdate(target, url, version, fullSHA string) (bak string, err error) {
	release, err := updateLock(target)
	if err != nil {
		return "", err
	}
	defer release()
	tmp, err := stageUpdate(target, url, version, fullSHA)
	if err != nil {
		return "", err
	}
	bak, err = swapIn(tmp, target)
	if err != nil {
		os.Remove(tmp) // the staged file never landed — don't leave a stray .new.<pid>
		return "", err
	}
	return bak, nil
}

// stageUpdate downloads url to a per-PID temp beside target (same dir => same fs => atomic
// rename), verifies it hashes to the advertised version (guards a mid-deploy race — and the
// full sha256 too when the server provides one), marks it executable, and smoke-tests it.
// On any failure the temp is removed: a failed update must not leave stale .new.<pid> files.
func stageUpdate(target, url, version, fullSHA string) (string, error) {
	tmp := fmt.Sprintf("%s.new.%d", target, os.Getpid())
	if err := downloadFile(url, tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if got := fileSHA12(tmp); got != version {
		os.Remove(tmp)
		return "", fmt.Errorf("downloaded %s != advertised %s (deploy in progress?)", got, version)
	}
	if fullSHA != "" {
		if got := fileSHA256(tmp); !strings.EqualFold(got, fullSHA) {
			os.Remove(tmp)
			return "", fmt.Errorf("downloaded sha256 %s != advertised %s", got, fullSHA)
		}
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return "", err
	}
	// Sanity-probe: the downloaded binary must actually run before we swap it in. A truncated/corrupt
	// download can pass the hash check when a partial publish was hashed-and-served self-consistently
	// (this is what bricked rbm21). Running `version` catches it BEFORE replacing the live binary.
	if err := probeBinary(tmp); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("downloaded binary rejected: %w", err)
	}
	return tmp, nil
}

// swapIn replaces target with the staged tmp per cli-update-spec §3 step 7: the current install
// moves to target+".bak" first, then tmp renames into place. If the second rename fails the .bak
// is restored — an update must never leave the tool without a runnable binary. The .bak is kept
// on success (the spec forbids deleting it — it's the operator's one-command rollback).
func swapIn(tmp, target string) (bak string, err error) {
	bak = target + ".bak"
	if err := os.Rename(target, bak); err != nil {
		return "", fmt.Errorf("backup %s -> %s: %w", target, bak, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		if rerr := os.Rename(bak, target); rerr != nil {
			return "", fmt.Errorf("swap failed (%v) and restoring %s failed too (%v) — recover by hand", err, bak, rerr)
		}
		return "", fmt.Errorf("swap failed, restored %s: %w", bak, err)
	}
	return bak, nil
}

// updateLock holds a non-blocking flock on <target>.lock across the swap so two mago processes
// sharing one binary path (several companies on a box) can't interleave their .bak moves
// (cli-update-spec §3: safe to run concurrently). The flock auto-releases if the holder dies —
// no stale-lock cleanup needed — and the lock file itself is left in place, since unlinking it
// would race a second opener.
func updateLock(target string) (func(), error) {
	f, err := os.OpenFile(target+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("%w (lock: %s)", errUpdateBusy, target+".lock")
	}
	return func() { f.Close() }, nil
}

// isPermErr reports whether err is a filesystem permission failure — the case where retrying
// cannot help until someone changes the directory's ownership or mode (the rbm4 loop).
func isPermErr(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}

// exePath is the running binary's path, for diagnostics.
func exePath() string {
	exe, err := os.Executable()
	if err != nil {
		return "the mago binary"
	}
	return exe
}

// currentUser names the account this process runs as, for "not writable by <user>" diagnostics.
func currentUser() string {
	if u, err := user.Current(); err == nil && strings.TrimSpace(u.Username) != "" {
		return u.Username
	}
	return fmt.Sprintf("uid %d", os.Getuid())
}

// probeBinary verifies a downloaded mago binary actually executes — `mago version` must run and print
// something. A truncated/corrupt file fails to exec or prints nothing, so this rejects it before it
// can replace the live binary (the belt-and-suspenders behind the hash check).
func probeBinary(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return fmt.Errorf("`version` self-probe failed to run: %w", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("`version` self-probe produced no output")
	}
	return nil
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
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}
