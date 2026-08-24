package main

import (
	"math"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestEnv(t *testing.T) {
	t.Setenv("MAGO_TEST_ENV", "from-env")
	if got := env("MAGO_TEST_ENV", "default"); got != "from-env" {
		t.Errorf("env(set) = %q, want %q", got, "from-env")
	}
	if got := env("MAGO_TEST_MISSING", "fallback"); got != "fallback" {
		t.Errorf("env(missing) = %q, want %q", got, "fallback")
	}
}

func TestExpand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := expand("~/data.db")
	want := home + "/data.db"
	if got != want {
		t.Errorf("expand(~/data.db) = %q, want %q", got, want)
	}

	if got := expand("/abs/path"); got != "/abs/path" {
		t.Errorf("expand(/abs/path) = %q, want %q", got, "/abs/path")
	}
}

func TestAtoi(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"42", 42},
		{"  7  ", 7},
		{"not-a-number", 0},
		{"", 0},
		{"-3", -3},
	}
	for _, c := range cases {
		got := atoi(c.in)
		if got != c.want {
			t.Errorf("atoi(%q) = %d, want %d", c.in, got, c.want)
		}
	}

	// Overflow saturates to MaxInt64 and the error is ignored, matching the
	// helper's best-effort contract.
	if got := atoi("999999999999999999999999999999999999"); got != math.MaxInt64 {
		t.Errorf("atoi(overflow) = %d, want %d", got, math.MaxInt64)
	}
}

func TestHttpErr(t *testing.T) {
	rec := httptest.NewRecorder()
	httpErr(rec, 418, "i am a teapot")

	if rec.Code != 418 {
		t.Errorf("status = %d, want 418", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"error":"i am a teapot"`) {
		t.Errorf("body = %q, want error message", body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// TestReadJSON verifies valid JSON is decoded into v and invalid JSON is rejected
// with a 400 response containing an "invalid json" error.
func TestReadJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api", strings.NewReader(`{"name":"mago"}`))
	var v struct{ Name string }
	if !readJSON(rec, req, &v) {
		t.Fatal("readJSON valid JSON returned false")
	}
	if v.Name != "mago" {
		t.Errorf("v.Name = %q, want mago", v.Name)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api", strings.NewReader(`not json`))
	var v2 struct{ Name string }
	if readJSON(rec2, req2, &v2) {
		t.Fatal("readJSON invalid JSON returned true")
	}
	if rec2.Code != 400 {
		t.Errorf("status = %d, want 400", rec2.Code)
	}
	body := rec2.Body.String()
	if !strings.Contains(body, "invalid json") {
		t.Errorf("body = %q, want invalid json", body)
	}
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, 201, map[string]string{"ok": "true"})

	if rec.Code != 201 {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"ok":"true"`) {
		t.Errorf("body = %q, want ok:true", body)
	}
}

func TestLoadDotenv(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/.env"
	content := `# comment
FOO=bar
  BAZ  =  "quoted"  
INVALID
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Clear any pre-existing value so loadDotenv can set it.
	os.Unsetenv("FOO")
	os.Unsetenv("BAZ")
	loadDotenv(path)

	if os.Getenv("FOO") != "bar" {
		t.Errorf("FOO = %q, want bar", os.Getenv("FOO"))
	}
	if os.Getenv("BAZ") != "quoted" {
		t.Errorf("BAZ = %q, want quoted", os.Getenv("BAZ"))
	}
	if os.Getenv("INVALID") != "" {
		t.Errorf("INVALID should not be set, got %q", os.Getenv("INVALID"))
	}

	// Already-set values are not overridden.
	t.Setenv("FOO", "preset")
	os.WriteFile(path, []byte("FOO=not-override\n"), 0o644)
	loadDotenv(path)
	if os.Getenv("FOO") != "preset" {
		t.Errorf("FOO was overridden to %q", os.Getenv("FOO"))
	}

	os.Unsetenv("FOO")
	os.Unsetenv("BAZ")
}

func TestLoadEnv(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/.env"
	if err := os.WriteFile(path, []byte("LOAD_ENV_FOO=from-mago\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Highest priority source wins.
	t.Setenv("MAGO_PLATFORM_ENV", path)
	os.Unsetenv("LOAD_ENV_FOO")
	loadEnv()
	if got := os.Getenv("LOAD_ENV_FOO"); got != "from-mago" {
		t.Errorf("LOAD_ENV_FOO = %q, want from-mago", got)
	}

	// Already-set values are not overridden by later files.
	t.Setenv("LOAD_ENV_FOO", "preset")
	loadEnv()
	if got := os.Getenv("LOAD_ENV_FOO"); got != "preset" {
		t.Errorf("LOAD_ENV_FOO was overridden to %q", got)
	}

	os.Unsetenv("LOAD_ENV_FOO")
}

func TestHandleSubscribed(t *testing.T) {
	cases := []struct {
		name   string
		query  string
		want   string
		nowant string
	}{
		{
			name:   "success",
			query:  "",
			want:   "Subscription active",
			nowant: "cancelled",
		},
		{
			name:   "cancelled",
			query:  "?cancelled=1",
			want:   "Checkout cancelled",
			nowant: "active",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/subscribed"+c.query, nil)
			handleSubscribed(rec, req)

			if rec.Code != 200 {
				t.Errorf("status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, c.want) {
				t.Errorf("body missing %q: %q", c.want, body)
			}
			if strings.Contains(body, c.nowant) {
				t.Errorf("body should not contain %q: %q", c.nowant, body)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
				t.Errorf("Content-Type = %q, want text/html", ct)
			}
		})
	}
}
