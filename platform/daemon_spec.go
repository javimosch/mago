package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// daemon_spec.go implements the cli-daemon-spec / cli-output-spec conformant lifecycle
// (`serve`, `daemon start|stop|status`, `/_health`, `/_shutdown`, `help-json`) ALONGSIDE the
// legacy `start`/`stop`/`status`/`restart` in daemon.go. The legacy commands are left
// untouched (same behavior, same tests) since hotify's config and the dk1 crontab already
// depend on their exact contract; this file adds the spec-conformant path rather than
// replacing the deployed one. New deploys/automation should prefer `daemon <action>`.
//
// See https://github.com/javimosch/cli-output-spec and
// https://github.com/javimosch/cli-daemon-spec.

// parseHostPort reads --host/--port (or MAGO_PLATFORM_HOST/PORT) for the new commands.
// Host defaults to loopback per cli-daemon-spec §6 -- a LAN/public bind must be explicit.
func parseHostPort(args []string) (host, port string) {
	host = env("MAGO_PLATFORM_HOST", "127.0.0.1")
	port = env("PORT", "9100")
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--host":
			if i+1 < len(args) {
				host = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--host="):
			host = strings.TrimPrefix(a, "--host=")
		case a == "--port" || a == "-port":
			if i+1 < len(args) {
				port = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--port="):
			port = strings.TrimPrefix(a, "--port=")
		case strings.HasPrefix(a, "-port="):
			port = strings.TrimPrefix(a, "-port=")
		}
	}
	return host, port
}

func isLoopbackHost(host string) bool {
	return host == "" || host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func shutdownTokenPath() string { return expand("~/.mago-platform/shutdown.token") }

// writeShutdownToken generates a fresh token on every serve/daemon-start (per spec §3) and
// persists it 0600 so daemon stop (a separate process invocation) can read it back.
func writeShutdownToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	if err := os.MkdirAll(filepath.Dir(shutdownTokenPath()), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(shutdownTokenPath(), []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

func readShutdownToken() string {
	b, _ := os.ReadFile(shutdownTokenPath())
	return strings.TrimSpace(string(b))
}

// handleHealth is the open, fast liveness probe per cli-daemon-spec §2.
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "service": "mago-platform", "pid": os.Getpid()})
}

// handleShutdown triggers a graceful exit(0) per §3. Token-gated only when bound off-loopback --
// a loopback-bound daemon can only ever be reached by a co-resident process.
func handleShutdown(host, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpErr(w, 405, "method not allowed")
			return
		}
		if !isLoopbackHost(host) {
			want := "Bearer " + token
			if token == "" || r.Header.Get("Authorization") != want {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(403)
				json.NewEncoder(w).Encode(map[string]bool{"ok": false})
				return
			}
		}
		writeJSON(w, 200, map[string]bool{"ok": true, "stopping": true})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go func() {
			time.Sleep(150 * time.Millisecond) // let the response flush before exiting
			os.Exit(0)
		}()
	}
}

func probeHealth(addr string, timeout time.Duration) bool {
	c := http.Client{Timeout: timeout}
	resp, err := c.Get("http://" + addr + "/_health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func logTail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// jsonOK prints a success payload to stdout (data only, per cli-output-spec §1) and exits 0.
func jsonOK(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

// jsonExit prints to stdout and exits with the given code (used for status's non-zero
// "stopped" steady state, which is success, not failure).
func jsonExit(code int, v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
	os.Exit(code)
}

// typedErrExit prints a typed error (cli-output-spec §3) to stdout and exits with its own
// code -- code and error.code always match, per spec.
func typedErrExit(code int, errType, message string, suggestions []string) {
	jsonExit(code, map[string]any{
		"ok": false,
		"error": map[string]any{
			"code":        code,
			"type":        errType,
			"message":     message,
			"recoverable": code >= 100 && code < 110,
			"suggestions": suggestions,
		},
	})
}

// cmdServe is the foreground primitive per cli-daemon-spec §1: blocks, binds loopback by
// default, prints one stderr confirmation line before entering the accept loop.
func cmdServe(args []string) error {
	host, port := parseHostPort(args)
	runServer(host, port)
	return nil
}

func cmdDaemon(args []string) error {
	if len(args) == 0 {
		typedErrExit(80, "invalid_arguments", "usage: mago-platform daemon <start|stop|status> [--host H] [--port P]", nil)
		return nil
	}
	action := args[0]
	host, port := parseHostPort(args[1:])
	switch action {
	case "start":
		return daemonStart(host, port)
	case "stop":
		return daemonStop(host, port)
	case "status":
		return daemonStatus(host, port)
	default:
		typedErrExit(80, "invalid_arguments", fmt.Sprintf("unknown daemon action %q", action), []string{"mago-platform daemon start|stop|status"})
		return nil
	}
}

func daemonStart(host, port string) error {
	addr := net.JoinHostPort(host, port)
	if probeHealth(addr, 300*time.Millisecond) {
		jsonOK(map[string]any{"ok": true, "daemon": "already_running", "host": host, "port": int(atoi(port))})
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logFile()), 0o755); err != nil {
		return err
	}
	lf, err := os.OpenFile(logFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	cmd := exec.Command(exe, "serve", "--host", host, "--port", port)
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = os.WriteFile(pidFile(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644) // observability parity with legacy start

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if probeHealth(addr, 200*time.Millisecond) {
			jsonOK(map[string]any{"ok": true, "daemon": "started", "host": host, "port": int(atoi(port)), "log": logFile()})
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	typedErrExit(100, "external_error", "child did not become healthy within 5s: "+logTail(logFile(), 20), nil)
	return nil
}

func daemonStop(host, port string) error {
	addr := net.JoinHostPort(host, port)
	if !probeHealth(addr, 300*time.Millisecond) {
		jsonOK(map[string]any{"ok": true, "daemon": "already_stopped"})
		return nil
	}
	req, _ := http.NewRequest(http.MethodPost, "http://"+addr+"/_shutdown", nil)
	if !isLoopbackHost(host) {
		req.Header.Set("Authorization", "Bearer "+readShutdownToken())
	}
	_, _ = http.DefaultClient.Do(req) // best-effort; the poll below is the real confirmation

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !probeHealth(addr, 200*time.Millisecond) {
			os.Remove(pidFile())
			jsonOK(map[string]any{"ok": true, "daemon": "stopped"})
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	typedErrExit(110, "internal_error", "daemon still responding after its own shutdown grace period", nil)
	return nil
}

func daemonStatus(host, port string) error {
	addr := net.JoinHostPort(host, port)
	if probeHealth(addr, 300*time.Millisecond) {
		jsonOK(map[string]any{"ok": true, "daemon": "running", "host": host, "port": int(atoi(port))})
		return nil
	}
	jsonExit(3, map[string]any{"ok": true, "daemon": "stopped"})
	return nil
}

// cmdHelpJSON is the self-describing command catalog per cli-output-spec §4.
func cmdHelpJSON() error {
	jsonOK(map[string]any{
		"version": "1.0.0",
		"output":  "json",
		"commands": map[string]any{
			"serve":        map[string]any{"args": []string{}, "flags": []string{"--host <h>", "--port <p>"}, "auth": false},
			"daemon":       map[string]any{"args": []string{"start|stop|status"}, "flags": []string{"--host <h>", "--port <p>"}, "auth": false},
			"start":        map[string]any{"args": []string{}, "flags": []string{"--port <p>", "--daemon"}, "auth": false, "note": "legacy PID-based lifecycle; prefer 'daemon start'"},
			"stop":         map[string]any{"args": []string{}, "flags": []string{}, "auth": false, "note": "legacy; prefer 'daemon stop'"},
			"restart":      map[string]any{"args": []string{}, "flags": []string{"--port <p>"}, "auth": false, "note": "legacy; prefer 'daemon stop' + 'daemon start'"},
			"status":       map[string]any{"args": []string{}, "flags": []string{}, "auth": false, "note": "legacy; prefer 'daemon status'"},
			"activity":     map[string]any{"args": []string{}, "flags": []string{}, "auth": true},
			"usage":        map[string]any{"args": []string{}, "flags": []string{}, "auth": true},
			"webhook":      map[string]any{"args": []string{}, "flags": []string{}, "auth": true},
			"setup-github": map[string]any{"args": []string{}, "flags": []string{}, "auth": true},
			"help-json":    map[string]any{"args": []string{}, "flags": []string{}, "auth": false},
		},
		"exit_codes": map[string]string{
			"0":   "success",
			"3":   "daemon status: stopped (steady state, not an error)",
			"80":  "input/validation",
			"90":  "precondition/resource",
			"100": "external/integration",
			"110": "internal",
		},
		"env": []string{
			"PORT", "MAGO_PLATFORM_HOST", "MAGO_PLATFORM_ENV", "DB_PATH", "JWT_SECRET",
			"STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET", "STRIPE_PRICE_MAGO", "APP_URL",
			"GITHUB_WEBHOOK_SECRET", "GITHUB_APP_ID", "GITHUB_ENFORCE_ENTITLEMENT",
		},
		"see_also": "https://github.com/javimosch/cli-guide-spec",
	})
	return nil
}
