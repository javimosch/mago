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

// TestHandleFeedback_Valid records a feedback event and returns ok when the
// message is non-empty and the request is authenticated.
func TestHandleFeedback_Valid(t *testing.T) {
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
	req := httptest.NewRequest("POST", "/api/feedback", strings.NewReader(`{"message":"  hello\nworld  ","type":"bug","version":"1.2.3","os":"linux"}`))
	req.Header.Set("Authorization", "Bearer "+jwtSign(secret, u.ID, u.Email))
	s.handleFeedback(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"ok":true`) {
		t.Errorf("body missing ok=true: %q", body)
	}
	if !strings.Contains(body, `"issue_url":""`) {
		t.Errorf("body missing empty issue_url: %q", body)
	}

	// The clipped log event should be recorded.
	var n int
	err = st.db.QueryRow("SELECT COUNT(*) FROM events WHERE kind='feedback' AND user_id=? AND detail LIKE '%bug%hello world%'", u.ID).Scan(&n)
	if err != nil || n != 1 {
		t.Fatalf("event not recorded as expected: err=%v count=%d", err, n)
	}
}

// TestHandleFeedback_DefaultType verifies that an omitted type falls back to "feedback"
// and is reflected in the recorded event.
func TestHandleFeedback_DefaultType(t *testing.T) {
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
	req := httptest.NewRequest("POST", "/api/feedback", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Authorization", "Bearer "+jwtSign(secret, u.ID, u.Email))
	s.handleFeedback(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	var n int
	err = st.db.QueryRow("SELECT COUNT(*) FROM events WHERE kind='feedback' AND user_id=? AND detail LIKE '[feedback] %'", u.ID).Scan(&n)
	if err != nil || n != 1 {
		t.Fatalf("event not recorded with default type: err=%v count=%d", err, n)
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
