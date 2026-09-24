package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
)

// session.go gives the browser a way to stay signed in after the SSO round trip.
//
// Deliberately NOT wired into authUID. The JSON API keeps requiring an Authorization header,
// so no state-changing endpoint can be driven by a cookie the browser attaches automatically
// — which removes CSRF from the API surface entirely rather than defending against it. The
// cookie exists only so server-rendered pages know who is looking at them.
//
// The two HTML actions that DO change state (mint a fresh setup code, sign out) are POSTs
// carrying a token derived from the session, below.

const sessionCookie = "mago_session"

func (s *server) setSession(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, Secure: strings.HasPrefix(s.appURL, "https://"),
		SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600,
	})
}

func (s *server) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/",
		HttpOnly: true, Secure: strings.HasPrefix(s.appURL, "https://"),
		SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// sessionUser returns the signed-in account for a page render, or nil.
func (s *server) sessionUser(r *http.Request) *User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	uid, ok := jwtVerify(s.jwtSecret, c.Value)
	if !ok {
		return nil
	}
	return s.store.GetByID(uid)
}

// formToken binds an HTML form to the session holding it. Same-site cookies already stop the
// cross-site POST in every browser that honours them; this is the belt to that suspenders,
// and it is one HMAC rather than a table of nonces.
func (s *server) formToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(s.jwtSecret))
	mac.Write([]byte("form:" + c.Value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *server) checkFormToken(r *http.Request) bool {
	want := s.formToken(r)
	return want != "" && hmac.Equal([]byte(want), []byte(r.FormValue("t")))
}
