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

	"golang.org/x/crypto/bcrypt"
)

// --- passwords (bcrypt, cost 12) ---

func hashPassword(pw string) string {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	if err != nil {
		return "" // empty hash can never match a login (CompareHashAndPassword fails)
	}
	return string(b)
}

func checkPassword(pw, stored string) bool {
	return bcrypt.CompareHashAndPassword([]byte(stored), []byte(pw)) == nil
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
	// Grant a no-card trial: issue the license now so the worker can connect immediately,
	// and start the 48h clock. `mago subscribe` converts to the paid plan.
	trialEnds := time.Now().Add(trialDuration).Unix()
	s.store.Update(u.ID, func(uu *User) {
		uu.Plan, uu.TrialEnds = "trial", trialEnds
		if uu.LicenseKey == "" {
			uu.LicenseKey = genLicense()
		}
	})
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
		"email": u.Email, "plan": u.Plan, "active": u.entitled(), "license_key": u.LicenseKey,
		"trial": u.trialActive(), "trial_ends": u.TrialEnds,
	})
}

func (s *server) authUID(r *http.Request) (int64, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return 0, false
	}
	return jwtVerify(s.jwtSecret, strings.TrimPrefix(h, "Bearer "))
}
