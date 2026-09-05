package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDebriComplete_RetryThenSuccess covers the retry/backoff path inside
// debriComplete when the first fake debri call fails (non-zero exit with empty
// content) and the second call succeeds. This exercises the `attempt > 0` sleep,
// the `err != nil` branch that records `lastErr`, and the eventual success return.
func TestDebriComplete_RetryThenSuccess(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "counter")
	if err := os.WriteFile(counter, []byte("0"), 0o644); err != nil {
		t.Fatalf("write counter: %v", err)
	}

	script := filepath.Join(dir, "debri")
	body := `#!/bin/sh
n=0
if [ -f "$COUNTER_FILE" ]; then
	read -r n < "$COUNTER_FILE"
fi
n=$((n + 1))
echo "$n" > "$COUNTER_FILE"
if [ "$n" -lt 2 ]; then
	echo '{"content":""}'
	exit 1
fi
echo '{"content":"ok"}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake debri: %v", err)
	}

	t.Setenv("PATH", dir)
	t.Setenv("COUNTER_FILE", counter)
	got, err := debriComplete(&Agent{Model: "SWE-1.6"}, "prompt")
	if err != nil {
		t.Fatalf("debriComplete: %v", err)
	}
	if got != "ok" {
		t.Errorf("debriComplete = %q, want ok", got)
	}

	b, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if strings.TrimSpace(string(b)) != "2" {
		t.Errorf("counter = %q, want 2", string(b))
	}
}

// TestDebriComplete_AllAttemptsFail covers the retry loop when every attempt fails with
// a different failure mode — a debri-reported {"error":...}, an empty-but-successful
// {"content":""}, and a non-zero exit — so each lastErr branch is exercised and the
// final failure is returned after the third attempt instead of retrying forever.
func TestDebriComplete_AllAttemptsFail(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "counter")
	if err := os.WriteFile(counter, []byte("0"), 0o644); err != nil {
		t.Fatalf("write counter: %v", err)
	}

	script := filepath.Join(dir, "debri")
	body := `#!/bin/sh
n=0
if [ -f "$COUNTER_FILE" ]; then
	read -r n < "$COUNTER_FILE"
fi
n=$((n + 1))
echo "$n" > "$COUNTER_FILE"
case "$n" in
1) echo '{"error":"devin wedged","elapsed_ms":1}' ;;
2) echo '{"content":""}' ;;
*) echo '{"content":""}'; exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake debri: %v", err)
	}

	t.Setenv("PATH", dir)
	t.Setenv("COUNTER_FILE", counter)

	_, err := debriComplete(&Agent{Model: "SWE-1.6"}, "prompt")
	if err == nil {
		t.Fatal("debriComplete: expected an error after all attempts fail, got nil")
	}
	if !strings.Contains(err.Error(), "debri failed") {
		t.Errorf("final error should be the last attempt's wait error, got: %v", err)
	}

	b, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if strings.TrimSpace(string(b)) != "3" {
		t.Errorf("counter = %q, want 3 (bounded retries)", string(b))
	}
}

// TestDebriComplete_PromptFileError verifies debriComplete wraps a prompt-file write
// failure (e.g. TMPDIR pointing at a missing directory) instead of exec'ing debri.
func TestDebriComplete_PromptFileError(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))

	_, err := debriComplete(&Agent{Model: "SWE-1.6"}, "prompt")
	if err == nil {
		t.Fatal("debriComplete: expected an error when the prompt file cannot be written")
	}
	if !strings.Contains(err.Error(), "writing prompt file") {
		t.Errorf("debriComplete should wrap the prompt-file error, got: %v", err)
	}
}
