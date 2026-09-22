package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

// resetpw.go is the operator's password-recovery path (see #264). mago-platform is driven
// entirely from the box it runs on -- by a human admin or an admin agent -- so "the account
// password is lost" has to be recoverable from this CLI rather than from a raw sqlite3 session.
//
// There is deliberately NO HTTP endpoint for this. The only trust boundary is filesystem access
// to the platform DB, exactly like the existing `activity` and `usage` verbs, so it adds no
// remote attack surface to a control plane that is otherwise reachable from the internet.
//
// Caveat, by design: JWTs are stateless (HS256 with a 30d exp, see auth.go), so a reset does NOT
// revoke tokens already issued -- they stay valid until they expire. Real session revocation
// would need a per-user token floor; that is a separate concern, not something this verb fakes.

// genPassword returns a random password strong enough that the generated-and-printed path is a
// reasonable default (144 bits), prefixed so it is recognizable in an operator's scrollback.
func genPassword() string {
	b := make([]byte, 18)
	rand.Read(b)
	return "mgp_" + base64.RawURLEncoding.EncodeToString(b)
}

func cmdResetPassword(args []string) error {
	email, pw := "", ""
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--email":
			if i+1 < len(args) {
				email = strings.TrimSpace(args[i+1])
				i++
			}
		case strings.HasPrefix(args[i], "--email="):
			email = strings.TrimSpace(strings.TrimPrefix(args[i], "--email="))
		case args[i] == "--password":
			if i+1 < len(args) {
				pw = args[i+1]
				i++
			}
		case strings.HasPrefix(args[i], "--password="):
			pw = strings.TrimPrefix(args[i], "--password=")
		}
	}
	if email == "" {
		typedErrExit(80, "invalid_arguments",
			"usage: mago-platform reset-password --email <email> [--password <pw>]",
			[]string{"mago-platform reset-password --email you@example.com"})
		return nil
	}

	st, err := openStore(expand(env("DB_PATH", "~/.mago-platform/platform.db")))
	if err != nil {
		typedErrExit(110, "internal_error", "open store: "+err.Error(), nil)
		return nil
	}
	defer st.Close()

	u := st.GetByEmail(email)
	if u == nil {
		typedErrExit(90, "not_found", fmt.Sprintf("no account with email %q", email),
			[]string{"mago-platform activity   # lists accounts"})
		return nil
	}

	generated := pw == ""
	if generated {
		pw = genPassword()
	}
	hash := hashPassword(pw)
	if hash == "" { // bcrypt refused (e.g. >72 bytes); an empty hash could never match a login
		typedErrExit(80, "invalid_arguments", "could not hash that password (bcrypt limit is 72 bytes)", nil)
		return nil
	}
	if err := st.SetPassword(u.ID, hash); err != nil {
		typedErrExit(110, "internal_error", "write password: "+err.Error(), nil)
		return nil
	}
	st.LogEvent("password_reset", u.ID, "operator CLI")

	out := map[string]any{"ok": true, "uid": u.ID, "email": u.Email, "generated": generated}
	if generated { // only echo what the caller does not already know
		out["password"] = pw
	}
	jsonOK(out)
	return nil
}
