package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEventIcon(t *testing.T) {
	cases := []struct {
		kind, want string
	}{
		{"signup", "🆕 signup"},
		{"subscribed", "💳 subscribed"},
		{"canceled", "✖ canceled"},
		{"worker_connect", "🟢 worker_connect"},
		{"worker_disconnect", "⚪ worker_disconnect"},
		{"linked", "🔗 linked"},
		{"unknown", "unknown"},
	}
	for _, c := range cases {
		got := eventIcon(c.kind)
		if got != c.want {
			t.Errorf("eventIcon(%q) = %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestCmdActivity_NoEvents(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "activity.db")
	t.Setenv("DB_PATH", db)

	out := captureStdout(t, func() {
		if err := cmdActivity(nil); err != nil {
			t.Fatalf("cmdActivity: %v", err)
		}
	})

	if !strings.Contains(out, "accounts: 0 total") {
		t.Errorf("output missing empty stats, got:\n%s", out)
	}
	if !strings.Contains(out, "(no activity recorded yet)") {
		t.Errorf("output missing no-activity message, got:\n%s", out)
	}
}

func TestCmdActivity_WithEvents(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "activity.db")
	st, err := openStore(db)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	u, err := st.Create("dev@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	st.LogEvent("signup", u.ID, "with one-click login")
	st.Close()

	t.Setenv("DB_PATH", db)
	out := captureStdout(t, func() {
		if err := cmdActivity(nil); err != nil {
			t.Fatalf("cmdActivity: %v", err)
		}
	})

	if !strings.Contains(out, "accounts: 1 total") {
		t.Errorf("output missing stats, got:\n%s", out)
	}
	if !strings.Contains(out, "dev@example.com") {
		t.Errorf("output missing email, got:\n%s", out)
	}
	if !strings.Contains(out, "signup") {
		t.Errorf("output missing event kind, got:\n%s", out)
	}
}

func TestCmdActivity_Limit(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "activity.db")
	st, err := openStore(db)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	u, err := st.Create("dev@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	st.LogEvent("signup", u.ID, "first-action")
	st.LogEvent("subscribed", u.ID, "second-action")
	st.Close()

	t.Setenv("DB_PATH", db)
	out := captureStdout(t, func() {
		if err := cmdActivity([]string{"--limit", "1"}); err != nil {
			t.Fatalf("cmdActivity: %v", err)
		}
	})

	if !strings.Contains(out, "second-action") {
		t.Errorf("output should include newest event, got:\n%s", out)
	}
	if strings.Contains(out, "first-action") {
		t.Errorf("output should not include older event when limit is 1, got:\n%s", out)
	}
}

func TestCmdActivity_PositionalLimit(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "activity.db")
	st, err := openStore(db)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	u, err := st.Create("dev@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	st.LogEvent("signup", u.ID, "first-action")
	st.LogEvent("subscribed", u.ID, "second-action")
	st.Close()

	t.Setenv("DB_PATH", db)
	out := captureStdout(t, func() {
		if err := cmdActivity([]string{"1"}); err != nil {
			t.Fatalf("cmdActivity: %v", err)
		}
	})

	if !strings.Contains(out, "second-action") {
		t.Errorf("output should include newest event, got:\n%s", out)
	}
	if strings.Contains(out, "first-action") {
		t.Errorf("output should not include older event when positional limit is 1, got:\n%s", out)
	}
}

func TestCmdActivity_LimitWithoutValue(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "activity.db")
	st, err := openStore(db)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	u, err := st.Create("dev@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	st.LogEvent("signup", u.ID, "first-action")
	st.LogEvent("subscribed", u.ID, "second-action")
	st.Close()

	t.Setenv("DB_PATH", db)
	out := captureStdout(t, func() {
		if err := cmdActivity([]string{"--limit"}); err != nil {
			t.Fatalf("cmdActivity: %v", err)
		}
	})

	// Default limit applies because the flag has no value.
	if !strings.Contains(out, "second-action") {
		t.Errorf("output should include newest event, got:\n%s", out)
	}
	if !strings.Contains(out, "first-action") {
		t.Errorf("output should include older event when default limit applies, got:\n%s", out)
	}
}

func TestCmdActivity_NonNumericPositional(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "activity.db")
	st, err := openStore(db)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	u, err := st.Create("dev@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	st.LogEvent("signup", u.ID, "first-action")
	st.Close()

	t.Setenv("DB_PATH", db)
	out := captureStdout(t, func() {
		if err := cmdActivity([]string{"abc"}); err != nil {
			t.Fatalf("cmdActivity: %v", err)
		}
	})

	if !strings.Contains(out, "first-action") {
		t.Errorf("output should include event when positional limit is non-numeric, got:\n%s", out)
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(b)
}
