package main

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestClip(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 6, "hello…"},
		{"  hello\nworld  ", 12, "hello world"},
		{"", 5, ""},
		{"exactly", 7, "exactly"},
	}
	for _, c := range cases {
		got := clip(c.in, c.n)
		if got != c.want {
			t.Errorf("clip(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestOrStr(t *testing.T) {
	if got := orStr("hello", "default"); got != "hello" {
		t.Errorf("orStr(\"hello\", \"default\") = %q, want \"hello\"", got)
	}
	if got := orStr("  ", "default"); got != "default" {
		t.Errorf("orStr(\"  \", \"default\") = %q, want \"default\"", got)
	}
	if got := orStr("", "default"); got != "default" {
		t.Errorf("orStr(\"\", \"default\") = %q, want \"default\"", got)
	}
}

// TestHandleFeedback_Unauthorized verifies that requests without a valid bearer token
// are rejected before any store or validation logic runs.
func TestHandleFeedback_Unauthorized(t *testing.T) {
	s := &server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/feedback", strings.NewReader(`{"message":"hello"}`))
	s.handleFeedback(rec, req)

	if rec.Code != 401 {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unauthorized") {
		t.Errorf("body = %q, want unauthorized", rec.Body.String())
	}
}

// TestHandleFeedback_EmptyMessage verifies that a request with only whitespace
// in the message body is rejected with a 400 error.
func TestHandleFeedback_EmptyMessage(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "feedback.db")
	st, err := openStore(db)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	defer st.Close()

	u, err := st.Create("dev@example.com", "hash")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	secret := "test-secret"
	s := &server{store: st, jwtSecret: secret}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/feedback", strings.NewReader(`{"message":"   "}`))
	req.Header.Set("Authorization", "Bearer "+jwtSign(secret, u.ID, u.Email))
	s.handleFeedback(rec, req)

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "message required") {
		t.Errorf("body = %q, want message required", rec.Body.String())
	}
}
