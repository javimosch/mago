package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWorkerPidFile(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	want := filepath.Join(dir, ".mago", "worker.pid")
	if got := workerPidFile(c); got != want {
		t.Errorf("workerPidFile() = %q, want %q", got, want)
	}
}

func TestWorkerLogFile(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}
	want := filepath.Join(dir, ".mago", "worker.log")
	if got := workerLogFile(c); got != want {
		t.Errorf("workerLogFile() = %q, want %q", got, want)
	}
}

func TestReadWorkerPid(t *testing.T) {
	dir := t.TempDir()
	c := &Company{Dir: dir}

	// Missing pidfile: not alive.
	if pid, alive := readWorkerPid(c); pid != 0 || alive {
		t.Errorf("missing pidfile: got pid=%d alive=%v, want 0/false", pid, alive)
	}

	// Malformed pidfile: not alive.
	magoDir := c.magoDir()
	if err := os.MkdirAll(magoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	pidFile := workerPidFile(c)
	if err := os.WriteFile(pidFile, []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	if pid, alive := readWorkerPid(c); pid != 0 || alive {
		t.Errorf("malformed pidfile: got pid=%d alive=%v, want 0/false", pid, alive)
	}

	// Current process is alive.
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	if pid, alive := readWorkerPid(c); pid != os.Getpid() || !alive {
		t.Errorf("current pid: got pid=%d alive=%v, want %d/true", pid, alive, os.Getpid())
	}

	// Non-existent process: not alive.
	if err := os.WriteFile(pidFile, []byte("999999\n"), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	if pid, alive := readWorkerPid(c); pid != 999999 || alive {
		t.Errorf("non-existent pid: got pid=%d alive=%v, want 999999/false", pid, alive)
	}
}

func TestStripArg(t *testing.T) {
	cases := []struct {
		name string
		args []string
		flag string
		want []string
	}{
		{"removes flag", []string{"mago", "serve", "--relay"}, "--relay", []string{"mago", "serve"}},
		{"no match", []string{"mago", "serve"}, "--daemon", []string{"mago", "serve"}},
		{"empty", []string{}, "--relay", []string{}},
		{"removes all occurrences", []string{"--relay", "mago", "--relay"}, "--relay", []string{"mago"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripArg(c.args, c.flag)
			if len(got) != len(c.want) {
				t.Fatalf("stripArg(%v, %q) = %v, want %v", c.args, c.flag, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("stripArg(%v, %q)[%d] = %q, want %q", c.args, c.flag, i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestWorkerStatus(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".mago"), 0o755); err != nil {
		t.Fatalf("mkdir .mago: %v", err)
	}
	t.Setenv("MAGO_GH_REPO", "")

	// No pidfile: worker is stopped.
	out := captureStdout(t, func() {
		if err := workerStatus(dir); err != nil {
			t.Fatalf("workerStatus stopped: %v", err)
		}
	})
	if !strings.Contains(out, "stopped") {
		t.Errorf("expected 'stopped', got: %q", out)
	}

	// Current process is alive.
	pidFile := filepath.Join(dir, ".mago", "worker.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	out = captureStdout(t, func() {
		if err := workerStatus(dir); err != nil {
			t.Fatalf("workerStatus running: %v", err)
		}
	})
	if !strings.Contains(out, "running") || !strings.Contains(out, "worker.log") {
		t.Errorf("expected 'running' and log path, got: %q", out)
	}
}

func TestWorkerStop_NoWorker(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".mago"), 0o755); err != nil {
		t.Fatalf("mkdir .mago: %v", err)
	}
	t.Setenv("MAGO_GH_REPO", "")

	err := workerStop(dir)
	if err == nil {
		t.Fatal("workerStop with no running worker should return an error")
	}
	if !strings.Contains(err.Error(), "no worker running") {
		t.Errorf("error = %q, want 'no worker running'", err.Error())
	}
}

func TestClassifyServeProc(t *testing.T) {
	dir := "/root/co-am"
	cases := []struct {
		name       string
		args       []string
		match      bool
		supervisor bool
	}{
		{"worker", []string{"/x/mago", "serve", "--relay", "-C", "/root/co-am"}, true, false},
		{"supervisor", []string{"/x/mago", "serve", "--supervise", "--relay", "-C", "/root/co-am"}, true, true},
		{"stop invocation excluded", []string{"/x/mago", "serve", "stop", "-C", "/root/co-am"}, false, false},
		{"prefix dir not matched (co-ampanel)", []string{"/x/mago", "serve", "--relay", "-C", "/root/co-ampanel"}, false, false},
		{"other company", []string{"/x/mago", "serve", "--relay", "-C", "/root/co-mago"}, false, false},
		{"not serve", []string{"/x/mago", "digest", "-C", "/root/co-am"}, false, false},
		{"-C without value", []string{"/x/mago", "serve", "--relay", "-C"}, false, false},
	}
	for _, c := range cases {
		m, s := classifyServeProc(c.args, dir)
		if m != c.match || (m && s != c.supervisor) {
			t.Errorf("%s: got match=%v sup=%v, want match=%v sup=%v", c.name, m, s, c.match, c.supervisor)
		}
	}
}

// writeStubExe creates an executable shell script at a temp path. The script is
// used in place of the mago binary so daemon/supervisor tests do not recurse
// into a real worker.
func writeStubExe(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "mago-stub.sh")
	if err := os.WriteFile(f, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return f
}

func TestDaemonizeWorker_AlreadyRunning(t *testing.T) {
	c := newTestCompany(t)
	if err := os.WriteFile(workerPidFile(c), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	err := daemonizeWorker(c, []string{})
	if err == nil {
		t.Fatal("expected error for running worker")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error = %q, want 'already running'", err.Error())
	}
}

func TestDaemonizeWorker_StartsSupervisor(t *testing.T) {
	c := newTestCompany(t)
	stub := writeStubExe(t, "exit 0")

	orig := executablePath
	defer func() { executablePath = orig }()
	executablePath = func() (string, error) { return stub, nil }

	out := captureStdout(t, func() {
		if err := daemonizeWorker(c, []string{}); err != nil {
			t.Fatalf("daemonizeWorker: %v", err)
		}
	})
	if !strings.Contains(out, "mago worker started") {
		t.Errorf("output = %q, want 'mago worker started'", out)
	}
	// The stub exits immediately, so just make sure we wrote a pidfile.
	if _, err := os.Stat(workerPidFile(c)); err != nil {
		t.Errorf("pidfile not written: %v", err)
	}
	os.Remove(workerPidFile(c))
}

func TestSuperviseWorker_CleanExit(t *testing.T) {
	stub := writeStubExe(t, "exit 0")

	orig := executablePath
	defer func() { executablePath = orig }()
	executablePath = func() (string, error) { return stub, nil }

	if err := superviseWorker([]string{}); err != nil {
		t.Fatalf("superviseWorker: %v", err)
	}
}

func TestWorkerStatus_InvalidCompany(t *testing.T) {
	dir := t.TempDir()
	err := workerStatus(dir)
	if err == nil {
		t.Fatal("workerStatus should error for a non-company directory")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error = %q, want 'not a mago company'", err.Error())
	}
}

func TestWorkerStop_InvalidCompany(t *testing.T) {
	dir := t.TempDir()
	err := workerStop(dir)
	if err == nil {
		t.Fatal("workerStop should error for a non-company directory")
	}
	if !strings.Contains(err.Error(), "not a mago company") {
		t.Errorf("error = %q, want 'not a mago company'", err.Error())
	}
}
