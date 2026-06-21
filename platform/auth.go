package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// --- passwords ---
// NOTE: placeholder salted iterated-SHA-256 KDF so the skeleton stays stdlib-only.
// Production should use bcrypt or argon2 (golang.org/x/crypto).

func hashPassword(pw string) string {
	salt := randBytes(16)
	return "s1$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(kdf(pw, salt))
}

func checkPassword(pw, stored string) bool {
	p := strings.Split(stored, "$")
	if len(p) != 3 {
		return false
	}
	salt, _ := hex.DecodeString(p[1])
	want, _ := hex.DecodeString(p[2])
	return hmac.Equal(kdf(pw, salt), want)
}

func kdf(pw string, salt []byte) []byte {
	cur := append([]byte{}, salt...)
	pwb := []byte(pw)
	for i := 0; i < 100000; i++ {
		msg := append(append(append([]byte{}, salt...), cur...), pwb...)
		h := sha256.Sum256(msg)
		cur = h[:]
	}
	return cur
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func genLicense() string { return "mago_" + hex.EncodeToString(randBytes(24)) }

// --- minimal HS256 JWT (stdlib) ---

func jwtSign(secret string, uid int64, email string) string {
	hdr := b64(`{"alg":"HS256","typ":"JWT"}`)
	claims, _ := json.Marshal(map[string]any{"uid": uid, "email": email, "exp": time.Now().Add(30 * 24 * time.Hour).Unix()})
	body := hdr + "." + b64(string(claims))
	return body + "." + sign(body, secret)
}

func jwtVerify(secret, token string) (int64, bool) {
	p := strings.Split(token, ".")
	if len(p) != 3 || !hmac.Equal([]byte(p[2]), []byte(sign(p[0]+"."+p[1], secret))) {
		return 0, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(p[1])
	if err != nil {
		return 0, false
	}
	var c struct {
		UID int64 `json:"uid"`
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &c) != nil || time.Now().Unix() > c.Exp {
		return 0, false
	}
	return c.UID, true
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func sign(msg, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// --- handlers ---

func (s *server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !readJSON(w, r, &in) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || len(in.Password) < 8 {
		httpErr(w, 400, "email required and password must be at least 8 characters")
		return
	}
	u, err := s.store.Create(email, hashPassword(in.Password))
	if err != nil {
		httpErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"token": jwtSign(s.jwtSecret, u.ID, u.Email)})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if !readJSON(w, r, &in) {
		return
	}
	u := s.store.GetByEmail(strings.ToLower(strings.TrimSpace(in.Email)))
	if u == nil || !checkPassword(in.Password, u.PasswordHash) {
		httpErr(w, 401, "invalid credentials")
		return
	}
	writeJSON(w, 200, map[string]string{"token": jwtSign(s.jwtSecret, u.ID, u.Email)})
}

func (s *server) handleAccount(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.authUID(r)
	if !ok {
		httpErr(w, 401, "unauthorized")
		return
	}
	u := s.store.GetByID(uid)
	if u == nil {
		httpErr(w, 404, "not found")
		return
	}
	writeJSON(w, 200, map[string]any{
		"email": u.Email, "plan": u.Plan, "active": u.Plan == "mago", "license_key": u.LicenseKey,
	})
}

func (s *server) authUID(r *http.Request) (int64, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return 0, false
	}
	return jwtVerify(s.jwtSecret, strings.TrimPrefix(h, "Bearer "))
}
