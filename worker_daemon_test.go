package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestSuperviseWorker_StartError(t *testing.T) {
	orig := executablePath
	defer func() { executablePath = orig }()
	executablePath = func() (string, error) { return filepath.Join(t.TempDir(), "no-such-binary"), nil }

	if err := superviseWorker([]string{}); err == nil {
		t.Fatal("superviseWorker should surface a start error")
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

func TestWorkerStop_StalePidfile(t *testing.T) {
	c := newTestCompany(t)
	t.Setenv("MAGO_GH_REPO", "")

	pidFile := workerPidFile(c)
	if err := os.WriteFile(pidFile, []byte("999999\n"), 0o644); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}

	out := captureStdout(t, func() {
		if err := workerStop(c.Dir); err != nil {
			t.Fatalf("workerStop: %v", err)
		}
	})
	if !strings.Contains(out, "0 process") {
		t.Errorf("output = %q, want '0 process'", out)
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Errorf("stale pidfile should be removed, got err = %v", err)
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

// TestWorkerStop_KillsRunningWorker exercises the branch of workerStop that finds
// and terminates a live worker and supervisor for the same company. It confirms
// killCompanyWorkers correctly classifies /proc entries and counts the killed
// processes without requiring a real mago binary.
func TestWorkerStop_KillsRunningWorker(t *testing.T) {
	c := newTestCompany(t)
	t.Setenv("MAGO_GH_REPO", "")

	stub := writeStubExe(t, "sleep 60")

	// Launch a supervisor and a worker for the same company directory.
	supervisor := exec.Command(stub, "serve", "--supervise", "-C", c.Dir)
	if err := supervisor.Start(); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	defer supervisor.Process.Kill()

	worker := exec.Command(stub, "serve", "-C", c.Dir)
	if err := worker.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	defer worker.Process.Kill()

	// Give the processes time to show up in /proc so killCompanyWorkers can see them.
	time.Sleep(200 * time.Millisecond)

	out := captureStdout(t, func() {
		if err := workerStop(c.Dir); err != nil {
			t.Fatalf("workerStop: %v", err)
		}
	})

	if !strings.Contains(out, "mago worker stopped") {
		t.Errorf("output = %q, want 'mago worker stopped'", out)
	}
	// Each stub shell spawned a child sleep, so killCompanyWorkers must kill the
	// matched processes and their descendants: 4 processes total.
	if !strings.Contains(out, "4 process") {
		t.Errorf("output = %q, want '4 process(es)'", out)
	}

	// Confirm both processes were reaped.
	for _, cmd := range []*exec.Cmd{supervisor, worker} {
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("process %d was not reaped", cmd.Process.Pid)
		}
	}
}

func TestDaemonizeWorker_StartError(t *testing.T) {
	c := newTestCompany(t)

	orig := executablePath
	defer func() { executablePath = orig }()
	// Return a path in an existing directory that does not name an executable,
	// so exec.Start fails before the supervisor is launched.
	executablePath = func() (string, error) { return filepath.Join(t.TempDir(), "no-such-binary"), nil }

	if err := daemonizeWorker(c, []string{}); err == nil {
		t.Fatal("daemonizeWorker should surface a start error")
	}
}

// TestSuperviseWorker_CrashRestart covers the non-zero-exit branch in superviseWorker:
// the worker crashes once, the supervisor logs and waits for the backoff, then it
// restarts and exits cleanly.
func TestSuperviseWorker_CrashRestart(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "counter")
	if err := os.WriteFile(counter, []byte("0"), 0o644); err != nil {
		t.Fatalf("write counter: %v", err)
	}

	script := `countf="${STUB_COUNTER}"
if [ ! -f "$countf" ]; then
	echo 0 > "$countf"
fi
n=$(cat "$countf")
n=$((n + 1))
echo "$n" > "$countf"
if [ "$n" -lt 2 ]; then
	exit 1
fi
exit 0`
	stub := writeStubExe(t, script)
	t.Setenv("STUB_COUNTER", counter)

	orig := executablePath
	defer func() { executablePath = orig }()
	executablePath = func() (string, error) { return stub, nil }

	if err := superviseWorker([]string{}); err != nil {
		t.Fatalf("superviseWorker: %v", err)
	}

	b, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("counter %q: %v", b, err)
	}
	if n < 2 {
		t.Fatalf("worker ran %d time(s), want at least 2", n)
	}
}

// TestChildPids_NoSuchPID verifies that a pid with no /proc entry yields nil rather
// than an error — childPids is a best-effort helper used during worker teardown.
func TestChildPids_NoSuchPID(t *testing.T) {
	if got := childPids(1 << 30); got != nil {
		t.Errorf("childPids(nonexistent) = %v, want nil", got)
	}
}

// TestKillProcessTree_Guards covers the early-return branches: non-positive pids and
// pids already recorded in killed must be skipped without signalling anything.
func TestKillProcessTree_Guards(t *testing.T) {
	killed := map[int]bool{}

	killProcessTree(0, killed)
	killProcessTree(-42, killed)
	if len(killed) != 0 {
		t.Fatalf("non-positive pids must not be killed, got %v", killed)
	}

	// A pid already in the map is a no-op (prevents cycles in /proc children).
	killed[os.Getpid()] = true
	killProcessTree(os.Getpid(), killed)
	if len(killed) != 1 {
		t.Fatalf("already-killed pid must not be re-processed, got %v", killed)
	}
}
