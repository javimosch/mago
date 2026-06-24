package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// worker_daemon.go gives the worker a native lifecycle: `mago serve --daemon` detaches a supervisor
// (per-company pidfile + logfile) that keeps the worker up — restarting it on crash, but exiting
// cleanly when the worker stops on its own (e.g. `--until`) or on `mago serve stop`. Per-company so a
// box can run several. (For OS-managed supervision instead, a systemd unit running the foreground
// `mago serve` with Restart=on-failure works too — see the `fleet` skill.)

func workerPidFile(c *Company) string { return filepath.Join(c.magoDir(), "worker.pid") }
func workerLogFile(c *Company) string { return filepath.Join(c.magoDir(), "worker.log") }

// readWorkerPid returns the supervisor pid from the company's pidfile and whether it's alive.
func readWorkerPid(c *Company) (int, bool) {
	b, err := os.ReadFile(workerPidFile(c))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, syscall.Kill(pid, 0) == nil // signal 0 probes existence
}

// daemonizeWorker detaches a supervisor process (re-exec with --supervise), writes the pidfile, and
// returns so the foreground command exits. cleanArgs is the serve args with --daemon removed.
func daemonizeWorker(c *Company, cleanArgs []string) error {
	if pid, alive := readWorkerPid(c); alive {
		return fmt.Errorf("worker already running for %q (pid %d) — `mago serve stop` first", c.Name, pid)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	lf, err := os.OpenFile(workerLogFile(c), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer lf.Close()
	cmd := exec.Command(exe, append([]string{"serve", "--supervise"}, cleanArgs...)...)
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach from this terminal/session
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := os.WriteFile(workerPidFile(c), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		return err
	}
	fmt.Printf("mago worker started for %q (supervisor pid %d) — logs: %s\n", c.Name, cmd.Process.Pid, workerLogFile(c))
	fmt.Printf("  stop: mago serve stop -C %s   status: mago serve status -C %s\n", c.Dir, c.Dir)
	return nil
}

// superviseWorker (internal --supervise mode) keeps a worker running: it spawns `mago serve` (the
// real foreground worker) and restarts it on crash, with backoff. A clean worker exit (code 0, e.g.
// --until) or SIGTERM stops the supervisor too. workerArgs is the serve args with --supervise removed.
func superviseWorker(workerArgs []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	backoff := time.Second
	for {
		cmd := exec.Command(exe, append([]string{"serve"}, workerArgs...)...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr // inherit the daemon logfile
		if err := cmd.Start(); err != nil {
			return err
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-sigs:
			fmt.Fprintln(os.Stderr, "[supervise] stop signal — terminating worker")
			cmd.Process.Signal(syscall.SIGTERM)
			cmd.Wait()
			return nil
		case werr := <-done:
			if werr == nil {
				fmt.Fprintln(os.Stderr, "[supervise] worker exited cleanly (e.g. --until) — done")
				return nil
			}
			fmt.Fprintf(os.Stderr, "[supervise] worker exited (%v) — restarting in %s\n", werr, backoff)
			select {
			case <-sigs:
				return nil
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
		}
	}
}

func workerStop(dir string) error {
	c, err := loadCompany(dir)
	if err != nil {
		return err
	}
	_, hadPidfile := readWorkerPid(c)
	os.Remove(workerPidFile(c))
	// A pidfile tracks only ONE supervisor; duplicates can accumulate (e.g. re-runs) and then fight
	// silently on the relay. So kill EVERY `mago serve … -C <this dir>` process — supervisors first
	// (so they can't respawn their worker), then any remaining workers.
	killed := killCompanyWorkers(c.Dir)
	if killed == 0 && !hadPidfile {
		return fmt.Errorf("no worker running for %q", c.Name)
	}
	fmt.Printf("mago worker stopped for %q (%d process(es))\n", c.Name, killed)
	return nil
}

// killCompanyWorkers SIGKILLs all `mago serve … -C <absDir>` processes (supervisors then workers),
// excluding this process and any `serve stop` invocation. Linux /proc-based; returns the count killed.
func killCompanyWorkers(absDir string) int {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return 0 // not Linux / no /proc — the pidfile path handled what it could
	}
	self := os.Getpid()
	var supers, workers []int
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		match, isSup := classifyServeProc(args, absDir)
		if !match {
			continue
		}
		if isSup {
			supers = append(supers, pid)
		} else {
			workers = append(workers, pid)
		}
	}
	for _, p := range supers { // kill supervisors first so they can't restart their child
		syscall.Kill(p, syscall.SIGKILL)
	}
	for _, p := range workers {
		syscall.Kill(p, syscall.SIGKILL)
	}
	return len(supers) + len(workers)
}

// classifyServeProc reports whether a process's argv is a `mago serve … -C <absDir>` worker/supervisor
// for exactly absDir (not a prefix — so co-am never matches co-ampanel), and whether it's the
// supervisor. A `serve stop` invocation is never matched (so `stop` can't target itself).
func classifyServeProc(args []string, absDir string) (match, supervisor bool) {
	var isServe, isStop, hasDir bool
	for i, a := range args {
		switch a {
		case "serve":
			isServe = true
		case "stop":
			isStop = true
		case "--supervise":
			supervisor = true
		case "-C":
			if i+1 < len(args) && args[i+1] == absDir {
				hasDir = true
			}
		}
	}
	return isServe && !isStop && hasDir, supervisor
}

func workerStatus(dir string) error {
	c, err := loadCompany(dir)
	if err != nil {
		return err
	}
	if pid, alive := readWorkerPid(c); alive {
		fmt.Printf("running (supervisor pid %d) — logs: %s\n", pid, workerLogFile(c))
	} else {
		fmt.Println("stopped")
	}
	return nil
}

// stripArg returns args with the given flag removed (the flag takes no value).
func stripArg(args []string, flag string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a != flag {
			out = append(out, a)
		}
	}
	return out
}
