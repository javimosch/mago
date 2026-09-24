package main

import (
	"fmt"
	"regexp"
	"strings"
)

// claim.go is the CLI half of web signup. Someone signs in with GitHub in a browser, the
// platform creates their account there and shows a single-use setup code; `mago claim <code>`
// exchanges it for the credentials and writes ~/.mago/config.json.
//
// The browser cannot write to this machine, so a code the human carries across is the handoff.
// It is short-lived, single-use, and rate-limited server-side, because it is a bearer
// credential for the duration it lives.

// claimCodeRe matches the format minted by the platform: MG-XXXX-XXXX over an alphabet with
// no 0/O/1/I. Checking the shape locally turns a typo into an instant, specific error instead
// of a round trip that comes back "not valid" and leaves the user unsure which part was wrong.
var claimCodeRe = regexp.MustCompile(`^MG-[ABCDEFGHJKLMNPQRSTUVWXYZ23456789]{4}-[ABCDEFGHJKLMNPQRSTUVWXYZ23456789]{4}$`)

func cmdClaim(args []string) error {
	var code string
	for _, a := range args {
		if a == "" || strings.HasPrefix(a, "-") {
			return &cliErr{80, fmt.Sprintf("unknown argument %q — usage: mago claim <code>", a)}
		}
		code = a
	}
	// People paste it with the surrounding whitespace and in whatever case they typed.
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return &cliErr{80, "usage: mago claim <code>   (the setup code shown after signing in at /signup)"}
	}
	if !claimCodeRe.MatchString(code) {
		return &cliErr{80, fmt.Sprintf("%q is not a setup code — they look like MG-7K2P-9QX4", code)}
	}

	cfg := loadConfig()
	var out struct {
		Token      string `json:"token"`
		Email      string `json:"email"`
		LicenseKey string `json:"license_key"`
		Plan       string `json:"plan"`
	}
	if err := cfg.platformDo("POST", "/api/claim", map[string]string{"code": code}, false, &out); err != nil {
		// The platform answers 404 for unknown, used and expired alike — it must not be an
		// oracle for guessing codes — so the advice covers all three.
		return &cliErr{90, fmt.Sprintf("%v\nget a fresh code by signing in again at %s/signup", err, cfg.PlatformURL)}
	}
	if out.Token == "" {
		return &cliErr{100, "the platform returned no token"}
	}

	cfg.Email, cfg.Token, cfg.LicenseKey = out.Email, out.Token, out.LicenseKey
	if err := cfg.save(); err != nil {
		return err
	}

	plan := out.Plan
	if plan == "founding" {
		plan = "founding operator — free during beta"
	}
	fmt.Printf("claimed %s (%s) — credentials saved to %s\n", out.Email, plan, configPath())
	fmt.Println("next: install the mago GitHub App on your repos, then `mago link --installation <id>`.")
	fmt.Println("      then `mago init ./company` and `mago serve --relay -C ./company`.")
	fmt.Println("      `mago worker doctor` checks your setup before you serve.")
	return nil
}
