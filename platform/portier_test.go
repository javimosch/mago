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

// TestPlaceholderEmailUpgrades: GitHub only reveals an address if the account made it public,
// so a first login can legitimately arrive with none and we store a placeholder to satisfy the
// UNIQUE column. When the IdP later supplies a real address, the account must adopt it —
// otherwise the operator stares at "@sso.invalid" forever.
func TestPlaceholderEmailUpgrades(t *testing.T) {
	s := newTestServer(t)

	first, _, err := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-100", Email: ""})
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	if !isPlaceholderEmail(first.Email) {
		t.Fatalf("an email-less login should get a placeholder, got %q", first.Email)
	}

	second, _, err := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-100", Email: "real@corp.com"})
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("must stay the same account: %d then %d", first.ID, second.ID)
	}
	if second.Email != "real@corp.com" {
		t.Errorf("placeholder should have been replaced, got %q", second.Email)
	}
}

// TestPlaceholderUpgradeNeverStealsAnAddress: upgrading must not take an address that already
// belongs to someone else — that would be the email-merge takeover by another route.
func TestPlaceholderUpgradeNeverStealsAnAddress(t *testing.T) {
	s := newTestServer(t)

	owner, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-owner", Email: "taken@corp.com"})
	ghost, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-ghost", Email: ""})
	if !isPlaceholderEmail(ghost.Email) {
		t.Fatalf("setup: expected a placeholder, got %q", ghost.Email)
	}

	after, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-ghost", Email: "taken@corp.com"})
	if after.Email == "taken@corp.com" {
		t.Error("ACCOUNT TAKEOVER: a placeholder upgrade claimed an address owned by another account")
	}
	if got := s.store.GetByID(owner.ID); got == nil || got.Email != "taken@corp.com" {
		t.Error("the original owner must keep its address")
	}
}

// TestSessionSurvivesNavigation is the gap this fixes: signing in, then visiting "/", used to
// look exactly like being signed out — the callback rendered one page and the site forgot you.
func TestSessionSurvivesNavigation(t *testing.T) {
	s := newTestServer(t)
	u, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-sess", Email: "sess@corp.com"})

	rec := httptest.NewRecorder()
	s.setSession(rec, jwtSign(s.jwtSecret, u.ID, u.Email))
	var c *http.Cookie
	for _, k := range rec.Result().Cookies() {
		if k.Name == sessionCookie {
			c = k
		}
	}
	if c == nil {
		t.Fatal("no session cookie was set")
	}
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Error("the session cookie must be HttpOnly and SameSite=Lax")
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(c)
	if got := s.sessionUser(req); got == nil || got.ID != u.ID {
		t.Fatalf("the session should identify the account, got %v", got)
	}

	// and the landing page should say so rather than inviting them to sign in again
	rec2 := httptest.NewRecorder()
	s.handleLanding(rec2, req)
	body := rec2.Body.String()
	if !strings.Contains(body, "/account") {
		t.Error("a signed-in visitor should be offered their account")
	}
	if strings.Contains(body, "{{NAV_") {
		t.Error("nav placeholders were left unsubstituted")
	}
}

// TestSessionIsNotAPIAuth: the cookie must never authenticate the JSON API. Keeping the two
// separate is what removes CSRF from the API surface rather than defending against it.
func TestSessionIsNotAPIAuth(t *testing.T) {
	s := newTestServer(t)
	u, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-api", Email: "api@corp.com"})

	req := httptest.NewRequest("GET", "/api/account", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: jwtSign(s.jwtSecret, u.ID, u.Email)})
	rec := httptest.NewRecorder()
	s.handleAccount(rec, req)

	if rec.Code != 401 {
		t.Errorf("a session cookie must not authenticate the API, got %d", rec.Code)
	}
}

// TestAccountActionsRequireFormToken: minting credentials and signing out are state-changing,
// so a bare cross-site POST must not drive them.
func TestAccountActionsRequireFormToken(t *testing.T) {
	s := newTestServer(t)
	u, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-csrf", Email: "csrf@corp.com"})
	cookie := &http.Cookie{Name: sessionCookie, Value: jwtSign(s.jwtSecret, u.ID, u.Email)}

	// no token -> refused, and no code minted
	req := httptest.NewRequest("POST", "/account/code", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.handleAccountCode(rec, req)
	if strings.Contains(rec.Body.String(), "MG-") {
		t.Error("a setup code was minted without a form token")
	}

	// with the token -> works
	tokReq := httptest.NewRequest("GET", "/account", nil)
	tokReq.AddCookie(cookie)
	tok := s.formToken(tokReq)
	if tok == "" {
		t.Fatal("no form token derived from the session")
	}
	req2 := httptest.NewRequest("POST", "/account/code", strings.NewReader("t="+tok))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	s.handleAccountCode(rec2, req2)
	if !strings.Contains(rec2.Body.String(), "MG-") {
		t.Errorf("a valid request should mint a code, got: %s", rec2.Body.String()[:200])
	}
}

// TestAccountRedirectsWhenSignedOut: no session, no account page.
func TestAccountRedirectsWhenSignedOut(t *testing.T) {
	s := newTestServer(t)
	rec := httptest.NewRecorder()
	s.handleAccountPage(rec, httptest.NewRequest("GET", "/account", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/signup" {
		t.Errorf("expected a redirect to /signup, got %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// TestWorkerNodesReconstructsFleet: the panel is derived from the event log rather than live
// sockets, so it survives a platform restart. Newest event per machine wins.
func TestWorkerNodesReconstructsFleet(t *testing.T) {
	s := newTestServer(t)
	u, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-nodes", Email: "nodes@corp.com"})

	s.store.LogEvent("worker_connect", u.ID, "alpha · repos=acme/one,acme/two")
	s.store.LogEvent("worker_connect", u.ID, "beta · repos=")
	s.store.LogEvent("worker_disconnect", u.ID, "alpha")

	nodes := s.store.WorkerNodes(u.ID, 10)
	byName := map[string]WorkerNode{}
	for _, n := range nodes {
		byName[n.Name] = n
	}
	if len(byName) != 2 {
		t.Fatalf("expected two machines, got %d: %+v", len(byName), nodes)
	}
	if byName["alpha"].Connected {
		t.Error("alpha's last event was a disconnect; it should read as offline")
	}
	if !byName["beta"].Connected {
		t.Error("beta never disconnected; it should read as connected")
	}
	if byName["alpha"].Repos != "acme/one,acme/two" {
		t.Errorf("repos not parsed off the connect detail: %q", byName["alpha"].Repos)
	}
	if byName["beta"].Repos != "" {
		t.Errorf("beta should have no repos, got %q", byName["beta"].Repos)
	}
}

// TestAccountPageShowsNodes: a worker with no repos is connected and idle forever, which looks
// like "working" from outside. The panel has to say so.
func TestAccountPageShowsNodes(t *testing.T) {
	s := newTestServer(t)
	u, _, _ := s.upsertSSOUser(&portierIdentity{Provider: "github", Sub: "gh-panel", Email: "panel@corp.com"})
	s.store.LogEvent("worker_connect", u.ID, "laptop-01 · repos=")

	req := httptest.NewRequest("GET", "/account", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: jwtSign(s.jwtSecret, u.ID, u.Email)})
	rec := httptest.NewRecorder()
	s.handleAccountPage(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Connected machines") {
		t.Error("the account page should show the fleet panel")
	}
	if !strings.Contains(body, "laptop-01") {
		t.Error("the connected machine should be listed")
	}
	if !strings.Contains(body, "mago link") {
		t.Error("a worker with no repos should be told how to fix it, not shown an empty cell")
	}
}
