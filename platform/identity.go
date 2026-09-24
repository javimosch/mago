package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// identity.go is the store layer for SSO accounts and the browser->CLI credential handoff.
//
// An SSO account is keyed on (provider, sub) — the IdP's stable subject — and NEVER on email.
// Email is display metadata. Matching on it would mean that any provider willing to report an
// address it has not verified could take over the account that already owns it, so a colliding
// email is reported to the operator rather than merged.

// ssoPasswordSentinel is written to users.password_hash for accounts that have no password.
// The column is NOT NULL, and a value that cannot be a bcrypt hash makes password login
// structurally impossible for these accounts rather than merely unlikely — bcrypt's compare
// rejects it as a malformed hash before any comparison happens.
const ssoPasswordSentinel = "!sso"

// claimCodeTTL is how long the code on the signup success page stays usable. Long enough to
// install the CLI on a slow connection, short enough that a screenshot or a shoulder-surfed
// terminal stops being a credential.
const claimCodeTTL = 15 * time.Minute

// Identity is one IdP login linked to a mago account.
type Identity struct {
	Provider string
	Sub      string
	UserID   int64
	Email    string
}

// GetIdentity returns the account linked to an IdP subject, or nil when this is a first login.
func (s *Store) GetIdentity(provider, sub string) *Identity {
	var i Identity
	err := s.db.QueryRow(
		"SELECT provider, sub, user_id, email FROM identities WHERE provider=? AND sub=?",
		provider, sub).Scan(&i.Provider, &i.Sub, &i.UserID, &i.Email)
	if err != nil {
		return nil
	}
	return &i
}

// LinkIdentity records that an IdP subject owns a mago account.
func (s *Store) LinkIdentity(provider, sub string, userID int64, email string) error {
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO identities (provider, sub, user_id, email, created_at) VALUES (?,?,?,?,?)",
		provider, sub, userID, email, time.Now().Unix())
	return err
}

// ssoEmailPlaceholder builds the address stored on an SSO account when the IdP gives us none,
// or when the real one already belongs to a different account. users.email is UNIQUE and is
// how operators recognise a row, so it has to be both unique and legible.
func ssoEmailPlaceholder(provider, sub string) string {
	h := sha256.Sum256([]byte(provider + ":" + sub))
	return fmt.Sprintf("%s-%s@sso.invalid", provider, hex.EncodeToString(h[:])[:12])
}

// isPlaceholderEmail reports whether an address is one we invented because the IdP gave us
// none. These exist only to satisfy the UNIQUE column; they are never deliverable and should
// be replaced the moment a real address turns up.
func isPlaceholderEmail(email string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(email)), "@sso.invalid")
}

// genClaimCode returns a short, human-transcribable single-use code: MG-XXXX-XXXX over an
// alphabet with no 0/O/1/I, because this is read off a web page and typed into a terminal.
// 32 bits of entropy is thin for a password and ample for a credential that lives 15 minutes,
// is single-use, and is rate-limited on exchange.
func genClaimCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	rand.Read(b) //nolint:errcheck // crypto/rand on a sane OS
	out := make([]byte, 8)
	for i, c := range b {
		out[i] = alphabet[int(c)%len(alphabet)]
	}
	return "MG-" + string(out[:4]) + "-" + string(out[4:])
}

func hashClaimCode(code string) string {
	h := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(code))))
	return hex.EncodeToString(h[:])
}

// MintClaimCode issues a single-use code that exchanges for this account's CLI credentials.
// Any unused codes for the account are dropped first: a visitor who restarts the signup flow
// should not leave a trail of live credentials behind them.
func (s *Store) MintClaimCode(userID int64) (string, error) {
	s.db.Exec("DELETE FROM claim_codes WHERE user_id=? AND used_at=0", userID) //nolint:errcheck
	code := genClaimCode()
	now := time.Now()
	_, err := s.db.Exec(
		"INSERT INTO claim_codes (code_hash, user_id, expires_at, created_at) VALUES (?,?,?,?)",
		hashClaimCode(code), userID, now.Add(claimCodeTTL).Unix(), now.Unix())
	if err != nil {
		return "", err
	}
	return code, nil
}

// RedeemClaimCode consumes a claim code and returns the account it belongs to. The same code
// can never be redeemed twice: the UPDATE is the guard, so two racing requests cannot both
// see an unused row and both succeed.
func (s *Store) RedeemClaimCode(code string) *User {
	res, err := s.db.Exec(
		"UPDATE claim_codes SET used_at=? WHERE code_hash=? AND used_at=0 AND expires_at>?",
		time.Now().Unix(), hashClaimCode(code), time.Now().Unix())
	if err != nil {
		return nil
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil // unknown, already used, or expired — deliberately indistinguishable
	}
	var uid int64
	if s.db.QueryRow("SELECT user_id FROM claim_codes WHERE code_hash=?", hashClaimCode(code)).Scan(&uid) != nil {
		return nil
	}
	return s.GetByID(uid)
}

// PurgeExpiredClaimCodes drops codes that can no longer be redeemed. Called opportunistically;
// correctness never depends on it, since RedeemClaimCode checks expiry itself.
func (s *Store) PurgeExpiredClaimCodes() {
	s.db.Exec("DELETE FROM claim_codes WHERE expires_at < ?", time.Now().Add(-24*time.Hour).Unix()) //nolint:errcheck
}
