package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSSONeverMergesByEmail is the security property this whole design turns on. Two different
// IdP subjects reporting the SAME email must NOT land on the same account — otherwise any
// provider willing to assert an address it has not verified could take over the account that
// already owns it. They get separate accounts and the operator is told.
func TestSSONeverMergesByEmail(t *testing.T) {
	s := newTestServer(t)

	first, note, err := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-1", Email: "alice@corp.com"})
	if err != nil {
		t.Fatalf("first signup: %v", err)
	}
	if note != "" {
		t.Errorf("a first signup should carry no collision note, got %q", note)
	}
	if first.Email != "alice@corp.com" {
		t.Errorf("first signup should keep its email, got %q", first.Email)
	}

	second, note, err := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-ATTACKER", Email: "alice@corp.com"})
	if err != nil {
		t.Fatalf("second signup: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("ACCOUNT TAKEOVER: a different IdP subject with the same email reached the same account")
	}
	if second.Email == "alice@corp.com" {
		t.Error("the colliding account must not claim the address that already belongs to another account")
	}
	if !strings.Contains(note, "never merge") {
		t.Errorf("the operator should be told a collision happened, got %q", note)
	}
}

// TestSSOReturningUserIsSameAccount: the same subject signing in twice is one account, not two.
func TestSSOReturningUserIsSameAccount(t *testing.T) {
	s := newTestServer(t)
	id := &portierIdentity{Provider: "github", Sub: "gh-42", Email: "bob@corp.com"}

	a, _, err := s.upsertSSOUser(id)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, _, err := s.upsertSSOUser(id)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a.ID != b.ID {
		t.Errorf("same (provider, sub) must resolve to one account, got %d then %d", a.ID, b.ID)
	}
}

// TestSSOAccountCannotPasswordLogin: an SSO account has no password, and the sentinel written
// to the NOT NULL column must not be loggable-in with — including by sending the sentinel
// itself as the password.
func TestSSOAccountCannotPasswordLogin(t *testing.T) {
	s := newTestServer(t)
	u, _, err := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-7", Email: "carol@corp.com"})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	for _, guess := range []string{ssoPasswordSentinel, "", "password", "!sso "} {
		if checkPassword(guess, u.PasswordHash) {
			t.Errorf("password %q must not authenticate an SSO account", guess)
		}
	}
}

// TestClaimCodeIsSingleUse: the code is a bearer credential; redeeming it twice must fail.
func TestClaimCodeIsSingleUse(t *testing.T) {
	s := newTestServer(t)
	u, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-9", Email: "dave@corp.com"})

	code, err := s.store.MintClaimCode(u.ID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if got := s.store.RedeemClaimCode(code); got == nil || got.ID != u.ID {
		t.Fatalf("first redeem should return the account, got %v", got)
	}
	if got := s.store.RedeemClaimCode(code); got != nil {
		t.Error("a claim code must not be redeemable twice")
	}
}

// TestClaimCodeExpires: an old code is dead even though it was never used.
func TestClaimCodeExpires(t *testing.T) {
	s := newTestServer(t)
	u, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-10", Email: "erin@corp.com"})
	code, _ := s.store.MintClaimCode(u.ID)

	// Reach into the row rather than sleeping 15 minutes.
	s.store.db.Exec("UPDATE claim_codes SET expires_at=? WHERE user_id=?", time.Now().Add(-time.Minute).Unix(), u.ID)

	if got := s.store.RedeemClaimCode(code); got != nil {
		t.Error("an expired claim code must not redeem")
	}
}

// TestMintClaimCodeInvalidatesPrevious: restarting signup must not leave live credentials
// scattered behind the visitor.
func TestMintClaimCodeInvalidatesPrevious(t *testing.T) {
	s := newTestServer(t)
	u, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-11", Email: "fred@corp.com"})

	old, _ := s.store.MintClaimCode(u.ID)
	fresh, _ := s.store.MintClaimCode(u.ID)
	if old == fresh {
		t.Fatal("expected a distinct code")
	}
	if got := s.store.RedeemClaimCode(old); got != nil {
		t.Error("minting a new code must invalidate the previous unused one")
	}
	if got := s.store.RedeemClaimCode(fresh); got == nil {
		t.Error("the newest code must still work")
	}
}

// TestClaimEndpointHidesWhyItFailed: unknown, used and expired must be indistinguishable, or
// the endpoint becomes an oracle for guessing codes.
func TestClaimEndpointHidesWhyItFailed(t *testing.T) {
	s := newTestServer(t)
	u, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-12", Email: "gina@corp.com"})
	used, _ := s.store.MintClaimCode(u.ID)
	s.store.RedeemClaimCode(used)

	bodies := map[string]string{}
	for _, code := range []string{used, "MG-ZZZZ-ZZZZ"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/claim", strings.NewReader(`{"code":"`+code+`"}`))
		req.Header.Set("Content-Type", "application/json")
		s.handleClaim(rec, req)
		if rec.Code != 404 {
			t.Errorf("code %q: got status %d, want 404", code, rec.Code)
		}
		bodies[code] = rec.Body.String()
	}
	if bodies[used] != bodies["MG-ZZZZ-ZZZZ"] {
		t.Errorf("a used code and an unknown code must be indistinguishable:\n used: %s\n unknown: %s",
			bodies[used], bodies["MG-ZZZZ-ZZZZ"])
	}
}

// TestClaimReturnsCredentials: the happy path hands the CLI everything it needs.
func TestClaimReturnsCredentials(t *testing.T) {
	s := newTestServer(t)
	u, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-13", Email: "hank@corp.com"})
	code, _ := s.store.MintClaimCode(u.ID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/claim", strings.NewReader(`{"code":"`+code+`"}`))
	req.Header.Set("Content-Type", "application/json")
	s.handleClaim(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"token"`, `"license_key"`, `"email"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("response missing %s: %s", want, rec.Body.String())
		}
	}
}

// TestPortierCallbackRejectsForeignState: a state echoed in the URL without the matching
// cookie is the CSRF case — an attacker can put anything in a query string, but cannot set a
// cookie on our origin.
func TestPortierCallbackRejectsForeignState(t *testing.T) {
	t.Setenv("MAGO_PORTIER_APP_ID", "app_test")
	t.Setenv("MAGO_PORTIER_SECRET", "psk_test")
	s := newTestServer(t)

	state := signState(s.jwtSecret, "nonce", time.Now().Add(time.Minute).Unix())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth/portier/callback?code=pc_x&state="+state, nil)
	// deliberately no cookie
	s.handlePortierCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("callback without the state cookie must be rejected, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "setup code") {
		t.Error("a rejected callback must not mint a claim code")
	}
}

// TestVerifyStateRejectsTamperedAndExpired covers the signature and the clock.
func TestVerifyStateRejectsTamperedAndExpired(t *testing.T) {
	secret := "test-secret"
	good := signState(secret, "nonce", time.Now().Add(time.Minute).Unix())
	if !verifyState(secret, good) {
		t.Fatal("a freshly signed state should verify")
	}
	if verifyState("other-secret", good) {
		t.Error("a state signed with a different secret must not verify")
	}
	if verifyState(secret, good+"x") {
		t.Error("a tampered state must not verify")
	}
	if verifyState(secret, signState(secret, "nonce", time.Now().Add(-time.Minute).Unix())) {
		t.Error("an expired state must not verify")
	}
	if verifyState(secret, "garbage") {
		t.Error("a malformed state must not verify")
	}
}

// TestPortierStartRejectsUnknownProvider: the provider segment goes into a URL path, so it is
// an allowlist rather than a passthrough.
func TestPortierStartRejectsUnknownProvider(t *testing.T) {
	t.Setenv("MAGO_PORTIER_APP_ID", "app_test")
	t.Setenv("MAGO_PORTIER_SECRET", "psk_test")
	s := newTestServer(t)

	for _, p := range []string{"evil", "../../etc", "google"} { // google is not enabled yet
		rec := httptest.NewRecorder()
		s.handlePortierStart(rec, httptest.NewRequest("GET", "/auth/portier/start?provider="+p, nil))
		if rec.Code == http.StatusFound {
			t.Errorf("provider %q must not start a login", p)
		}
	}
}

// TestPortierStartSetsStateCookie: the cookie is the CSRF defence, so its flags matter.
func TestPortierStartSetsStateCookie(t *testing.T) {
	t.Setenv("MAGO_PORTIER_APP_ID", "app_test")
	t.Setenv("MAGO_PORTIER_SECRET", "psk_test")
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	s.handlePortierStart(rec, httptest.NewRequest("GET", "/auth/portier/start?provider=github", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("expected a redirect, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, portierBase+"/auth/app_test/github") {
		t.Errorf("unexpected redirect target: %s", loc)
	}
	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "mago_sso_state" {
			found = c
		}
	}
	if found == nil {
		t.Fatal("no state cookie was set")
	}
	if !found.HttpOnly {
		t.Error("the state cookie must be HttpOnly")
	}
	if found.SameSite != http.SameSiteLaxMode {
		t.Error("the state cookie must be SameSite=Lax so it survives the IdP redirect but not a cross-site POST")
	}
}

// TestSignupPageDegradesWhenUnconfigured: an unconfigured deployment should say so, not 500.
func TestSignupPageDegradesWhenUnconfigured(t *testing.T) {
	t.Setenv("MAGO_PORTIER_APP_ID", "")
	t.Setenv("MAGO_PORTIER_SECRET", "")
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	s.handleSignupPage(rec, httptest.NewRequest("GET", "/signup", nil))
	if rec.Code != 200 {
		t.Errorf("status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "mago register") {
		t.Error("an unconfigured signup page should point at the CLI path")
	}
}

// TestSanitizeForHTML: IdP-supplied strings reach the page by concatenation, so this is the
// guard that keeps them from breaking out of it.
func TestSanitizeForHTML(t *testing.T) {
	got := sanitizeForHTML(`<script>alert("x")</script>`)
	for _, bad := range []string{"<", ">", `"`} {
		if strings.Contains(got, bad) {
			t.Errorf("sanitized output still contains %q: %s", bad, got)
		}
	}
	if len(sanitizeForHTML(strings.Repeat("a", 500))) > 200 {
		t.Error("sanitized output must be length-capped")
	}
}

// newTestServer builds a platform server over a throwaway SQLite file.
func newTestServer(t *testing.T) *server {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "portier.db"))
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return &server{store: st, jwtSecret: "test-secret", appURL: "https://mago.example"}
}
