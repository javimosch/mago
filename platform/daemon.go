package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
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
		runServer(env("MAGO_PLATFORM_HOST", "127.0.0.1"), port) // foreground; blocks (hotify mode)
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

// cmdRestart stops whatever is serving and starts a fresh daemonized server. Unlike stop+start it
// frees the port by killing the actual LISTENER — not just the pidfile pid — so a stale server
// started outside the pidfile path (a manual nohup, an old deploy) can't keep holding the port while
// the "new" process silently fails to bind. After it returns, the pidfile is correct so stop/status
// work again.
func cmdRestart(args []string) error {
	port, _ := parseStartFlags(args)
	// Terminate the pidfile process if any.
	if pid, alive := readPid(); alive {
		syscall.Kill(pid, syscall.SIGTERM)
	}
	os.Remove(pidFile())
	// Free the port: SIGTERM the listener (whoever it is), waiting up to ~5s for it to exit.
	var owner int
	for i := 0; i < 50; i++ {
		if owner = pidOnPort(port); owner == 0 {
			break
		}
		syscall.Kill(owner, syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
	}
	if owner = pidOnPort(port); owner != 0 { // stubborn — escalate
		fmt.Printf("port %s still held by pid %d — SIGKILL\n", port, owner)
		syscall.Kill(owner, syscall.SIGKILL)
		time.Sleep(300 * time.Millisecond)
	}
	if owner = pidOnPort(port); owner != 0 {
		return fmt.Errorf("port %s still held by pid %d after SIGKILL — restart aborted", port, owner)
	}
	return cmdStart([]string{"--daemon", "--port", port})
}

// pidOnPort returns the PID LISTENING on the given TCP port (v4 or v6), or 0. Pure-Go via /proc so
// it needs no ss/lsof/fuser on the host.
func pidOnPort(port string) int {
	p, err := strconv.Atoi(port)
	if err != nil || p <= 0 {
		return 0
	}
	inode := listenInode(p)
	if inode == "" {
		return 0
	}
	want := "socket:[" + inode + "]"
	fds, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	for _, fd := range fds {
		if link, err := os.Readlink(fd); err == nil && link == want {
			if parts := strings.Split(fd, "/"); len(parts) >= 3 {
				if pid, err := strconv.Atoi(parts[2]); err == nil {
					return pid
				}
			}
		}
	}
	return 0
}

// listenInode scans /proc/net/tcp{,6} for the socket inode in LISTEN state on the given port.
func listenInode(port int) string {
	suffix := fmt.Sprintf(":%04X", port) // local_address column ends with :PORT (uppercase hex)
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lines := strings.Split(string(b), "\n")
		for _, line := range lines[1:] { // skip the header row
			fields := strings.Fields(line)
			// 1=local_address (HEXIP:HEXPORT), 3=state (0A=LISTEN), 9=inode
			if len(fields) < 10 || fields[3] != "0A" {
				continue
			}
			if strings.HasSuffix(fields[1], suffix) {
				return fields[9]
			}
		}
	}
	return ""
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
