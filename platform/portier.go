package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// portier.go implements web signup via portier.intrane.fr, which brokers the OAuth dance with
// GitHub (and later Google/Intrane) and hands back a verified identity. We implement no OAuth:
// no per-provider client secrets, no token refresh, no provider-specific callbacks.
//
// The flow, and why each step is shaped this way:
//
//	/auth/portier/start?provider=github
//	  -> mint CSRF state, put it in an HttpOnly cookie AND in the redirect, 302 to portier
//	/auth/portier/callback?code=&state=
//	  -> state must match the cookie (an attacker can echo a state in a URL; they cannot
//	     set a cookie on our origin), then exchange the code server-side for the identity
//	  -> upsert the account, mint a claim code, render the two commands
//
// The claim code exists because the credential has to reach ~/.mago/config.json on a machine
// the browser cannot write to. The account is fully created before the CLI is ever installed —
// that is the point of the whole feature.

const portierBase = "https://portier.intrane.fr"

// portierProviders is the allowlist of providers we will start a login with. It is an
// allowlist rather than a passthrough so the provider segment of the portier URL can never be
// attacker-controlled.
var portierProviders = map[string]string{"github": "GitHub"}

// stateTTL bounds how long a signup can sit half-finished. Short: the only thing it protects
// is a round trip through an IdP.
const stateTTL = 10 * time.Minute

func portierAppID() string  { return strings.TrimSpace(env("MAGO_PORTIER_APP_ID", "")) }
func portierSecret() string { return strings.TrimSpace(env("MAGO_PORTIER_SECRET", "")) }

// portierConfigured reports whether web signup is switched on. Unconfigured is a normal state
// (local dev, a fresh deploy): the routes answer with a clear message instead of a 500.
func portierConfigured() bool { return portierAppID() != "" && portierSecret() != "" }

// signState signs "<nonce>.<expiry>" with the platform's JWT secret. The cookie carries the
// same value, so the callback verifies both that we issued the state and that it was issued
// to this browser.
func signState(secret, nonce string, exp int64) string {
	payload := fmt.Sprintf("%s.%d", nonce, exp)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyState(secret, state string) bool {
	parts := strings.Split(state, ".")
	if len(parts) != 3 {
		return false
	}
	var exp int64
	if _, err := fmt.Sscanf(parts[1], "%d", &exp); err != nil || time.Now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(signState(secret, parts[0], exp)), []byte(state))
}

// handlePortierStart begins a login: mint state, set it as a cookie, redirect to portier.
func (s *server) handlePortierStart(w http.ResponseWriter, r *http.Request) {
	if !portierConfigured() {
		httpErr(w, 503, "web signup is not configured on this deployment")
		return
	}
	provider := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	if provider == "" {
		provider = "github"
	}
	if _, ok := portierProviders[provider]; !ok {
		httpErr(w, 400, "unsupported provider "+provider)
		return
	}

	nonce := hex.EncodeToString(randBytes(16))
	state := signState(s.jwtSecret, nonce, time.Now().Add(stateTTL).Unix())
	http.SetCookie(w, &http.Cookie{
		Name: "mago_sso_state", Value: state, Path: "/",
		HttpOnly: true, Secure: strings.HasPrefix(s.appURL, "https://"),
		SameSite: http.SameSiteLaxMode, MaxAge: int(stateTTL.Seconds()),
	})

	redirect := strings.TrimRight(s.appURL, "/") + "/auth/portier/callback"
	dest := fmt.Sprintf("%s/auth/%s/%s?redirect_uri=%s&state=%s",
		portierBase, url.PathEscape(portierAppID()), url.PathEscape(provider),
		url.QueryEscape(redirect), url.QueryEscape(state))
	http.Redirect(w, r, dest, http.StatusFound)
}

// portierIdentity is the verified identity portier returns from /v1/token.
type portierIdentity struct {
	Sub      string `json:"sub"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// exchangePortierCode trades the one-time code for the identity, server-side with our secret.
func exchangePortierCode(code string) (*portierIdentity, error) {
	body, _ := json.Marshal(map[string]string{"code": code})
	req, err := http.NewRequest("POST", portierBase+"/v1/token", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+portierSecret())
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("portier token exchange failed (%d)", resp.StatusCode)
	}
	var out struct {
		Identity portierIdentity `json:"identity"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("portier returned an unreadable identity")
	}
	if strings.TrimSpace(out.Identity.Sub) == "" {
		return nil, fmt.Errorf("portier returned an identity with no subject")
	}
	return &out.Identity, nil
}

// handlePortierCallback completes a login and renders the hand-off page.
func (s *server) handlePortierCallback(w http.ResponseWriter, r *http.Request) {
	if !portierConfigured() {
		httpErr(w, 503, "web signup is not configured on this deployment")
		return
	}
	// An IdP error comes back as ?error= and must not be treated as a login attempt.
	if e := r.URL.Query().Get("error"); e != "" {
		s.renderSignupError(w, "The identity provider refused the login ("+sanitizeForHTML(e)+"). Nothing was created.")
		return
	}
	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie("mago_sso_state")
	if err != nil || cookie.Value == "" || !hmac.Equal([]byte(cookie.Value), []byte(state)) || !verifyState(s.jwtSecret, state) {
		// Either the state was not ours, or it was not issued to this browser.
		s.renderSignupError(w, "That sign-in link is no longer valid. Please start again.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "mago_sso_state", Value: "", Path: "/", MaxAge: -1})

	code := r.URL.Query().Get("code")
	if code == "" {
		s.renderSignupError(w, "The sign-in did not return a code. Please start again.")
		return
	}
	id, err := exchangePortierCode(code)
	if err != nil {
		s.renderSignupError(w, "We could not verify that sign-in. Please start again.")
		return
	}

	u, note, err := s.upsertSSOUser(id)
	if err != nil {
		s.renderSignupError(w, sanitizeForHTML(err.Error()))
		return
	}
	claim, err := s.store.MintClaimCode(u.ID)
	if err != nil {
		s.renderSignupError(w, "Your account exists, but we could not issue a setup code. Try signing in again.")
		return
	}
	s.store.PurgeExpiredClaimCodes()
	// Keep the browser signed in, so "/" and a later visit recognise them and a fresh setup
	// code is one click away instead of another trip through the IdP.
	s.setSession(w, jwtSign(s.jwtSecret, u.ID, u.Email))
	s.renderClaim(w, u, claim, note)
}

// upsertSSOUser finds or creates the account behind a verified identity.
//
// Lookup is by (provider, sub) only. If the IdP's email already belongs to a DIFFERENT
// account, we do NOT merge: a provider that reports an unverified address would otherwise be
// able to take over an existing account. We create a distinct account with a placeholder
// address and tell the operator, which is less tidy and not takeoverable.
func (s *server) upsertSSOUser(id *portierIdentity) (*User, string, error) {
	if ident := s.store.GetIdentity(id.Provider, id.Sub); ident != nil {
		if u := s.store.GetByID(ident.UserID); u != nil {
			// A first login can arrive with no email — GitHub only reveals one if the
			// account made it public — and we store a placeholder so the UNIQUE column
			// holds. Once the IdP does hand us a real address, adopt it, or the operator
			// is stuck looking at "@sso.invalid" forever. Still never across accounts:
			// only if the address is free.
			if isPlaceholderEmail(u.Email) {
				if real := strings.ToLower(strings.TrimSpace(id.Email)); real != "" {
					if other := s.store.GetByEmail(real); other == nil {
						if err := s.store.SetEmail(u.ID, real); err == nil {
							s.store.LinkIdentity(id.Provider, id.Sub, u.ID, real) //nolint:errcheck
							s.store.LogEvent("email_resolved", u.ID, "placeholder replaced by "+id.Provider+" address")
							u = s.store.GetByID(u.ID)
						}
					}
				}
			}
			return u, "", nil
		}
	}

	email := strings.ToLower(strings.TrimSpace(id.Email))
	note := ""
	if email == "" {
		email = ssoEmailPlaceholder(id.Provider, id.Sub)
	} else if existing := s.store.GetByEmail(email); existing != nil {
		note = "An account already uses " + sanitizeForHTML(email) +
			". This is a separate account — we never merge accounts by email address."
		email = ssoEmailPlaceholder(id.Provider, id.Sub)
	}

	u, err := s.store.Create(email, ssoPasswordSentinel)
	if err != nil {
		return nil, "", fmt.Errorf("could not create the account")
	}
	// Same entitlement rules as handleSignup: the first real external operators are founding,
	// everyone else gets the no-card trial, and a license is issued now so the worker can
	// connect the moment the CLI is installed.
	plan, trialEnds := "trial", time.Now().Add(trialDuration).Unix()
	founding := !isInternalEmail(email) && s.store.FoundingCount() < foundingCap
	if founding {
		plan, trialEnds = "founding", 0
	}
	s.store.Update(u.ID, func(uu *User) {
		uu.Plan, uu.TrialEnds = plan, trialEnds
		if uu.LicenseKey == "" {
			uu.LicenseKey = genLicense()
		}
	})
	s.store.LinkIdentity(id.Provider, id.Sub, u.ID, email) //nolint:errcheck
	detail := "48h trial via " + id.Provider
	if founding {
		detail = "FOUNDING operator via " + id.Provider
	}
	s.store.LogEvent("signup", u.ID, detail)
	return s.store.GetByID(u.ID), note, nil
}

// handleClaim is the CLI side of the hand-off: exchange a claim code for credentials.
func (s *server) handleClaim(w http.ResponseWriter, r *http.Request) {
	var in struct{ Code string }
	if !readJSON(w, r, &in) {
		return
	}
	code := strings.ToUpper(strings.TrimSpace(in.Code))
	if code == "" {
		httpErr(w, 400, "code required")
		return
	}
	u := s.store.RedeemClaimCode(code)
	if u == nil {
		// One message for unknown, used and expired: distinguishing them turns the endpoint
		// into an oracle for guessing codes.
		httpErr(w, 404, "that setup code is not valid — it may have expired or already been used")
		return
	}
	s.store.LogEvent("claimed", u.ID, "cli claimed web signup")
	writeJSON(w, 200, map[string]string{
		"token":       jwtSign(s.jwtSecret, u.ID, u.Email),
		"email":       u.Email,
		"license_key": u.LicenseKey,
		"plan":        u.Plan,
	})
}

// sanitizeForHTML strips the characters that would let an IdP-supplied string break out of the
// text it is rendered into. The pages below are built by concatenation, so this is the guard.
func sanitizeForHTML(s string) string {
	r := strings.NewReplacer("<", "", ">", "", "&", "", "\"", "", "'", "", "\n", " ", "\r", " ")
	out := r.Replace(s)
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}
