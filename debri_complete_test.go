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
