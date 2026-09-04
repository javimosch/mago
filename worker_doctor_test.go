package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkerDoctor_GHRepoTokenFailure verifies that workerDoctor includes the
// MAGO_GH_TOKEN check when MAGO_GH_REPO is set and exits 101 when the token is
// missing. The test runs in a subprocess so the os.Exit(101) does not terminate
// the main test runner.
func TestWorkerDoctor_GHRepoTokenFailure(t *testing.T) {
	if strings.TrimSpace(os.Getenv("MAGO_TEST_WORKER_DOCTOR_GH_REPO_CHILD")) == "1" {
		dir := t.TempDir()
		for _, name := range []string{"tau", "gh"} {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		t.Setenv("PATH", dir)
		t.Setenv("MAGO_PROVIDER", "")
		t.Setenv("MAGO_GH_REPO", "owner/repo")
		t.Setenv("MAGO_GH_TOKEN", "")
		t.Setenv("OPENCODE_API_KEY", "secret")
		workerDoctor()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestWorkerDoctor_GHRepoTokenFailure")
	cmd.Env = append(os.Environ(), "MAGO_TEST_WORKER_DOCTOR_GH_REPO_CHILD=1")
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("workerDoctor did not exit the process: %v", err)
	}
	if exit.ExitCode() != 101 {
		t.Fatalf("workerDoctor exit code = %d, want 101", exit.ExitCode())
	}
}
