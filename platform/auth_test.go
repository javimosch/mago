package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestB64(t *testing.T) {
	got := b64(`{"alg":"HS256","typ":"JWT"}`)
	want := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	if got != want {
		t.Errorf("b64() = %q, want %q", got, want)
	}
}

func TestSign(t *testing.T) {
	msg := "header.payload"
	secret := "shh"
	got := sign(msg, secret)
	want := base64.RawURLEncoding.EncodeToString(hmacSHA256(msg, secret))
	if got != want {
		t.Errorf("sign(%q, %q) = %q, want %q", msg, secret, got, want)
	}
}

func hmacSHA256(msg, secret string) []byte {
	// duplicate the private sign logic for an independent reference
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return m.Sum(nil)
}

func TestJWTSignVerify(t *testing.T) {
	secret := "test-secret"
	uid := int64(42)
	email := "dev@example.com"

	token := jwtSign(secret, uid, email)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwtSign produced %d parts, want 3", len(parts))
	}

	gotUID, ok := jwtVerify(secret, token)
	if !ok {
		t.Fatalf("jwtVerify rejected a freshly signed token")
	}
	if gotUID != uid {
		t.Errorf("jwtVerify uid = %d, want %d", gotUID, uid)
	}

	// tampered signature should fail
	if _, ok := jwtVerify(secret, token+"x"); ok {
		t.Error("jwtVerify accepted a tampered token")
	}
	if _, ok := jwtVerify("wrong-secret", token); ok {
		t.Error("jwtVerify accepted a token with the wrong secret")
	}

	// Expired token should fail.
	expiredPayload, _ := json.Marshal(map[string]any{"uid": uid, "email": email, "exp": int64(1)})
	expiredBody := b64(`{"alg":"HS256","typ":"JWT"}`) + "." + b64(string(expiredPayload))
	expiredToken := expiredBody + "." + sign(expiredBody, secret)
	if _, ok := jwtVerify(secret, expiredToken); ok {
		t.Error("jwtVerify accepted an expired token")
	}
}

func TestJWTVerify_MalformedPayload(t *testing.T) {
	secret := "test-secret"

	header := b64(`{"alg":"HS256","typ":"JWT"}`)

	// Payload segment is not valid base64 — decoding must fail.
	badB64 := header + ".not-valid-base64!!!." + sign(header+".not-valid-base64!!!", secret)
	if _, ok := jwtVerify(secret, badB64); ok {
		t.Error("jwtVerify accepted a token with an un-decodable payload")
	}

	// Payload segment is valid base64 but not JSON — unmarshaling must fail.
	badJSON := header + "." + b64("not json") + "." + sign(header+"."+b64("not json"), secret)
	if _, ok := jwtVerify(secret, badJSON); ok {
		t.Error("jwtVerify accepted a token with a non-JSON payload")
	}
}

func TestGenLicense(t *testing.T) {
	k := genLicense()
	if !strings.HasPrefix(k, "mago_") {
		t.Errorf("genLicense() = %q, want 'mago_' prefix", k)
	}
	hexPart := strings.TrimPrefix(k, "mago_")
	if len(hexPart) != 48 {
		t.Errorf("genLicense() hex part length = %d, want 48", len(hexPart))
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		t.Errorf("genLicense() hex part not valid hex: %v", err)
	}
}

func TestHashPasswordAndCheck(t *testing.T) {
	hashed := hashPassword("hunter2")
	if hashed == "" {
		t.Fatal("hashPassword returned empty string")
	}
	if hashed == "hunter2" {
		t.Error("hashPassword returned plaintext")
	}

	if !checkPassword("hunter2", hashed) {
		t.Error("checkPassword rejected the correct password")
	}
	if checkPassword("wrong", hashed) {
		t.Error("checkPassword accepted an incorrect password")
	}

	// An empty hash can never match a login.
	if checkPassword("hunter2", "") {
		t.Error("checkPassword accepted an empty hash")
	}
}

// TestHandleLogin verifies successful and failed login attempts.
func TestHandleLogin(t *testing.T) {
	dir := t.TempDir()
	st, err := openStore(filepath.Join(dir, "login.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	u, err := st.Create("dev@example.com", hashPassword("hunter2"))
	if err != nil {
		t.Fatal(err)
	}

	secret := "test-secret"
	srv := &server{store: st, jwtSecret: secret}

	t.Run("success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"email":"dev@example.com","password":"hunter2"}`))
		srv.handleLogin(rec, req)

		if rec.Code != 200 {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `"token":"`) {
			t.Errorf("body missing token: %q", body)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"email":"dev@example.com","password":"wrong"}`))
		srv.handleLogin(rec, req)

		if rec.Code != 401 {
			t.Errorf("status = %d, want 401", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "invalid credentials") {
			t.Errorf("body = %q, want invalid credentials", rec.Body.String())
		}
	})

	t.Run("unknown email", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"email":"missing@example.com","password":"hunter2"}`))
		srv.handleLogin(rec, req)

		if rec.Code != 401 {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`not json`))
		srv.handleLogin(rec, req)

		if rec.Code != 400 {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("empty password", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"email":"dev@example.com","password":""}`))
		srv.handleLogin(rec, req)

		if rec.Code != 401 {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	// Sanity check the created user matches expectations.
	if u.ID == 0 {
		t.Error("created user has zero id")
	}
}

// TestHandleAccount verifies the /api/account handler returns user details for
// valid sessions and rejects missing or invalid bearer tokens.
func TestHandleAccount(t *testing.T) {
	dir := t.TempDir()
	st, err := openStore(filepath.Join(dir, "account.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	u, err := st.Create("dev@example.com", hashPassword("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	st.Update(u.ID, func(uu *User) { uu.Plan = "founding" })

	secret := "test-secret"
	srv := &server{store: st, jwtSecret: secret}

	t.Run("success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/account", nil)
		req.Header.Set("Authorization", "Bearer "+jwtSign(secret, u.ID, u.Email))
		srv.handleAccount(rec, req)

		if rec.Code != 200 {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `"email":"dev@example.com"`) {
			t.Errorf("body missing email: %q", body)
		}
		if !strings.Contains(body, `"plan":"founding"`) {
			t.Errorf("body missing plan: %q", body)
		}
		if !strings.Contains(body, `"active":true`) {
			t.Errorf("body missing active: %q", body)
		}
	})

	t.Run("missing token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/account", nil)
		srv.handleAccount(rec, req)

		if rec.Code != 401 {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/account", nil)
		req.Header.Set("Authorization", "Bearer not-a-token")
		srv.handleAccount(rec, req)

		if rec.Code != 401 {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("tampered token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/account", nil)
		req.Header.Set("Authorization", "Bearer "+jwtSign(secret, u.ID, u.Email)+"x")
		srv.handleAccount(rec, req)

		if rec.Code != 401 {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	// A valid token for a user that no longer exists in the store must 404.
	t.Run("not found", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/account", nil)
		req.Header.Set("Authorization", "Bearer "+jwtSign(secret, 999, "missing@example.com"))
		srv.handleAccount(rec, req)

		if rec.Code != 404 {
			t.Errorf("status = %d, want 404", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "not found") {
			t.Errorf("body = %q, want not found", rec.Body.String())
		}
	})
}

// TestHandleSignup verifies validation, duplicate detection, and both the trial
// and founding signup flows.
func TestHandleSignup(t *testing.T) {
	dir := t.TempDir()
	st, err := openStore(filepath.Join(dir, "signup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	secret := "test-secret"
	srv := &server{store: st, jwtSecret: secret}

	t.Run("success external", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/signup", strings.NewReader(`{"email":"dev@startup.io","password":"hunter2000"}`))
		srv.handleSignup(rec, req)

		if rec.Code != 200 {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `"token":"`) {
			t.Errorf("body missing token: %q", body)
		}

		u := st.GetByEmail("dev@startup.io")
		if u == nil {
			t.Fatal("user not created")
		}
		if u.Plan != "founding" {
			t.Errorf("plan = %q, want founding for first external signup", u.Plan)
		}
		if u.LicenseKey == "" {
			t.Error("license key not set")
		}
	})

	t.Run("success internal", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/signup", strings.NewReader(`{"email":"dev@example.com","password":"hunter2000"}`))
		srv.handleSignup(rec, req)

		if rec.Code != 200 {
			t.Errorf("status = %d, want 200", rec.Code)
		}

		u := st.GetByEmail("dev@example.com")
		if u == nil {
			t.Fatal("user not created")
		}
		if u.Plan != "trial" {
			t.Errorf("plan = %q, want trial for internal signup", u.Plan)
		}
	})

	t.Run("duplicate email", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/signup", strings.NewReader(`{"email":"dev@example.com","password":"hunter2000"}`))
		srv.handleSignup(rec, req)

		if rec.Code != 409 {
			t.Errorf("status = %d, want 409", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "email") {
			t.Errorf("body = %q, want duplicate email error", rec.Body.String())
		}
	})

	t.Run("missing email", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/signup", strings.NewReader(`{"email":"   ","password":"hunter2000"}`))
		srv.handleSignup(rec, req)

		if rec.Code != 400 {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("short password", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/signup", strings.NewReader(`{"email":"new@startup.io","password":"short"}`))
		srv.handleSignup(rec, req)

		if rec.Code != 400 {
			t.Errorf("status = %d, want 400", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "at least 8") {
			t.Errorf("body = %q, want password length error", rec.Body.String())
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/signup", strings.NewReader(`not json`))
		srv.handleSignup(rec, req)

		if rec.Code != 400 {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})
}
