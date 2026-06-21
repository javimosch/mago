package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// daemon.go gives mago-platform a start/stop/status lifecycle. Under hotify the app runs in the
// foreground (`mago-platform start --port N`) and hotify supervises it. `start --daemon` is the
// standalone path: it detaches, writes a pidfile, and logs to a file, so `stop`/`status` work.

func pidFile() string { return expand("~/.mago-platform/mago-platform.pid") }
func logFile() string { return expand("~/.mago-platform/mago-platform.log") }

// parsePort accepts `--port N`, `-port N`, `--port=N`, `-port=N`.
func parseStartFlags(args []string) (port string, daemonize bool) {
	port = env("PORT", "9100")
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--daemon" || a == "-d":
			daemonize = true
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
	return port, daemonize
}

func cmdStart(args []string) error {
	port, daemonize := parseStartFlags(args)
	if !daemonize {
		runServer(port) // foreground; blocks (hotify mode)
		return nil
	}
	if pid, alive := readPid(); alive {
		return fmt.Errorf("already running (pid %d)", pid)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(pidFile()), 0o755); err != nil {
		return err
	}
	lf, err := os.OpenFile(logFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	cmd := exec.Command(exe, "start", "--port", port)
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach from this process group
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := os.WriteFile(pidFile(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		return err
	}
	fmt.Printf("mago-platform started (pid %d, port %s) — logs: %s\n", cmd.Process.Pid, port, logFile())
	return nil
}

func cmdStop() error {
	pid, alive := readPid()
	if !alive {
		os.Remove(pidFile())
		return fmt.Errorf("not running")
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return err
	}
	os.Remove(pidFile())
	fmt.Printf("mago-platform stopped (pid %d)\n", pid)
	return nil
}

func cmdStatus() error {
	pid, alive := readPid()
	if !alive {
		fmt.Println("stopped")
		return nil
	}
	fmt.Printf("running (pid %d)\n", pid)
	return nil
}

// readPid returns the pidfile's pid and whether that process is alive.
func readPid() (int, bool) {
	b, err := os.ReadFile(pidFile())
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	// signal 0 probes existence without killing.
	return pid, syscall.Kill(pid, 0) == nil
}
