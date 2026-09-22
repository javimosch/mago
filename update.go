package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// update.go implements `mago update` — the operator-facing self-update verb (cli-update-spec §3):
// check → download → verify → smoke-test → atomic swap with .bak rollback. It shares machinery
// with the worker's update=auto mode (selfupdate.go); the difference is who drives it — a human
// or agent runs this on demand, while auto mode fires off the relay ping unprompted.

// serverVersion is the platform's §2 response: which artifact it currently serves for this os/arch.
type serverVersion struct {
	OK       bool   `json:"ok"`
	Version  string `json:"version"`
	Download string `json:"download"`
	SHA256   string `json:"sha256"`
	Error    string `json:"error"`
}

// url resolves the artifact URL: Download may be a server-relative path or an absolute URL;
// when absent we fall back to the well-known /dl/mago route for this os/arch.
func (sv *serverVersion) url(base string) string {
	if sv.Download != "" {
		if strings.HasPrefix(sv.Download, "http://") || strings.HasPrefix(sv.Download, "https://") {
			return sv.Download
		}
		return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(sv.Download, "/")
	}
	return strings.TrimRight(base, "/") + "/dl/mago?os=" + runtime.GOOS + "&arch=" + runtime.GOARCH
}

// fetchServerVersion asks the platform which CLI version it currently serves for this os/arch
// (GET /version — cli-update-spec §2; open endpoint, no auth).
func fetchServerVersion() (*serverVersion, error) {
	base := strings.TrimRight(loadConfig().PlatformURL, "/")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(base + "/version?os=" + runtime.GOOS + "&arch=" + runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var sv serverVersion
	json.Unmarshal(raw, &sv)
	if resp.StatusCode != 200 {
		if sv.Error != "" {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, sv.Error)
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if !sv.OK || sv.Version == "" {
		return nil, fmt.Errorf("malformed /version response: %s", truncate(string(raw), 120))
	}
	return &sv, nil
}

// cmdUpdate is `mago update [--check] [--force]`. Progress goes to stderr; the machine-readable
// result goes to stdout. Exit codes per spec: 0 ok · 5 = --check found an update (not an error) ·
// 80 bad flags · 90 the binary's directory isn't writable by this user · 100 update failure.
func cmdUpdate(args []string) error {
	check, force := false, false
	for _, a := range args {
		switch a {
		case "--check":
			check = true
		case "--force":
			force = true
		default:
			return &cliErr{80, fmt.Sprintf("unknown flag %q — usage: mago update [--check] [--force]", a)}
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cfg := loadConfig()
	local := selfVersion()
	sv, err := fetchServerVersion()
	if err != nil {
		return &cliErr{100, fmt.Sprintf("cannot fetch the server version from %s: %v", cfg.PlatformURL, err)}
	}
	if check {
		out, _ := json.Marshal(map[string]any{
			"ok": true, "local": local, "remote": sv.Version, "up_to_date": local == sv.Version,
		})
		fmt.Println(string(out))
		if local != sv.Version {
			return &cliErr{5, ""} // update available — §3: exit 5, and it's not an error
		}
		return nil
	}
	if local == sv.Version && !force {
		out, _ := json.Marshal(map[string]any{
			"ok": true, "updated": false, "up_to_date": true, "version": local,
		})
		fmt.Println(string(out))
		return nil
	}
	fmt.Fprintf(os.Stderr, "[update] %s → %s; downloading…\n", local, sv.Version)
	bak, err := runUpdate(exe, sv.url(cfg.PlatformURL), sv.Version, sv.SHA256)
	if err != nil {
		if errors.Is(err, errUpdateBusy) {
			return &cliErr{100, fmt.Sprintf("update aborted: %v", err)}
		}
		if isPermErr(err) {
			return &cliErr{90, fmt.Sprintf("cannot write %s as %s — install to a writable prefix "+
				"(`mago install --prefix ~/.local/bin`) or fix directory permissions: %v",
				filepath.Dir(exe), currentUser(), err)}
		}
		return &cliErr{100, fmt.Sprintf("update failed: %v", err)}
	}
	fmt.Fprintf(os.Stderr, "[update] updated %s → %s (backup: %s)\n", local, sv.Version, bak)
	out, _ := json.Marshal(map[string]any{
		"ok": true, "updated": true, "from": local, "to": sv.Version, "backup": bak,
	})
	fmt.Println(string(out))
	return nil
}
