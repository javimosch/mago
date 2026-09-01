package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseStartFlags(t *testing.T) {
	t.Run("default port from env", func(t *testing.T) {
		t.Setenv("PORT", "8080")
		port, daemon := parseStartFlags(nil)
		if port != "8080" {
			t.Errorf("default port = %q, want 8080", port)
		}
		if daemon {
			t.Error("default daemon = true, want false")
		}
	})

	t.Run("--port flag", func(t *testing.T) {
		port, _ := parseStartFlags([]string{"--port", "3000"})
		if port != "3000" {
			t.Errorf("--port = %q, want 3000", port)
		}
	})

	t.Run("--port= flag", func(t *testing.T) {
		port, _ := parseStartFlags([]string{"--port=3001"})
		if port != "3001" {
			t.Errorf("--port= = %q, want 3001", port)
		}
	})

	t.Run("-port flag", func(t *testing.T) {
		port, _ := parseStartFlags([]string{"-port", "3002"})
		if port != "3002" {
			t.Errorf("-port = %q, want 3002", port)
		}
	})

	t.Run("-port= flag", func(t *testing.T) {
		port, _ := parseStartFlags([]string{"-port=3003"})
		if port != "3003" {
			t.Errorf("-port= = %q, want 3003", port)
		}
	})

	t.Run("--daemon flag", func(t *testing.T) {
		_, daemon := parseStartFlags([]string{"--daemon"})
		if !daemon {
			t.Error("--daemon = false, want true")
		}
	})

	t.Run("-d flag", func(t *testing.T) {
		_, daemon := parseStartFlags([]string{"-d"})
		if !daemon {
			t.Error("-d = false, want true")
		}
	})

	t.Run("mixed flags", func(t *testing.T) {
		port, daemon := parseStartFlags([]string{"-d", "--port", "4000"})
		if port != "4000" {
			t.Errorf("mixed port = %q, want 4000", port)
		}
		if !daemon {
			t.Error("mixed daemon = false, want true")
		}
	})
}

// TestCmdStatus verifies the platform daemon status command prints stopped when
// no pidfile exists and running when the pidfile points to a live process.
func TestCmdStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// No pidfile: stopped.
	out := captureStdout(t, func() {
		if err := cmdStatus(); err != nil {
			t.Fatalf("cmdStatus stopped: %v", err)
		}
	})
	if !strings.Contains(out, "stopped") {
		t.Errorf("expected 'stopped', got: %q", out)
	}

	// Current process: running.
	pidPath := pidFile()
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}

	out = captureStdout(t, func() {
		if err := cmdStatus(); err != nil {
			t.Fatalf("cmdStatus running: %v", err)
		}
	})
	if !strings.Contains(out, "running") || !strings.Contains(out, strconv.Itoa(os.Getpid())) {
		t.Errorf("expected 'running' with pid, got: %q", out)
	}
}

// TestReadPid verifies missing/malformed pidfiles return not-alive, and the
// current process is recognized as alive.
func TestReadPid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Missing pidfile: not alive.
	if pid, alive := readPid(); pid != 0 || alive {
		t.Errorf("missing pidfile: got pid=%d alive=%v, want 0/false", pid, alive)
	}

	// Malformed pidfile: not alive.
	pidPath := pidFile()
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pidPath, []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	if pid, alive := readPid(); pid != 0 || alive {
		t.Errorf("malformed pidfile: got pid=%d alive=%v, want 0/false", pid, alive)
	}

	// Current process is alive.
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	if pid, alive := readPid(); pid != os.Getpid() || !alive {
		t.Errorf("current pid: got pid=%d alive=%v, want %d/true", pid, alive, os.Getpid())
	}

	// Non-existent process: not alive.
	if err := os.WriteFile(pidPath, []byte("999999\n"), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	if pid, alive := readPid(); pid != 999999 || alive {
		t.Errorf("non-existent pid: got pid=%d alive=%v, want 999999/false", pid, alive)
	}
}

// TestPidFileAndLogFile verify the daemon file helpers expand ~/ to the user's
// home directory and point at the expected mago-platform run directory.
func TestPidFileAndLogFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := pidFile(); got != filepath.Join(home, ".mago-platform", "mago-platform.pid") {
		t.Errorf("pidFile() = %q, want %q", got, filepath.Join(home, ".mago-platform", "mago-platform.pid"))
	}
	if got := logFile(); got != filepath.Join(home, ".mago-platform", "mago-platform.log") {
		t.Errorf("logFile() = %q, want %q", got, filepath.Join(home, ".mago-platform", "mago-platform.log"))
	}
}
