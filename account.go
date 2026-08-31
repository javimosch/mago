package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// account.go is the client side of the platform API: register/login/subscribe/account.
// It is a thin HTTP client holding NO operator secrets (those live in platform/), so it is
// safe to open-source with the rest of the core. See docs/SAAS.md.

const defaultPlatformURL = "http://localhost:9100"

// cliConfig is persisted at ~/.mago/config.json (0600), rcmd-style.
type cliConfig struct {
	PlatformURL string `json:"platform_url"`
	Email       string `json:"email,omitempty"`
	Token       string `json:"token,omitempty"`       // JWT from the platform
	LicenseKey  string `json:"license_key,omitempty"` // issued on first payment; used by the worker
}

func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".mago-config.json"
	}
	return filepath.Join(home, ".mago", "config.json")
}

func loadConfig() *cliConfig {
	c := &cliConfig{PlatformURL: defaultPlatformURL}
	if b, err := os.ReadFile(configPath()); err == nil {
		json.Unmarshal(b, c)
	}
	if u := strings.TrimSpace(os.Getenv("MAGO_PLATFORM_URL")); u != "" {
		c.PlatformURL = u
	}
	if c.PlatformURL == "" {
		c.PlatformURL = defaultPlatformURL
	}
	return c
}

func (c *cliConfig) save() error {
	p := configPath()
	if err := ensureDir(filepath.Dir(p)); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

// platformDo performs a JSON request against the platform API. If authed, it attaches the
// stored JWT and returns a friendly error when the token is missing.
func (c *cliConfig) platformDo(method, path string, in any, authed bool, out any) error {
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.PlatformURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if authed {
		if c.Token == "" {
			return fmt.Errorf("not logged in — run `mago register` or `mago login` first")
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach platform at %s: %w", c.PlatformURL, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(raw, &e)
		if e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("platform returned %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// --- credential input ---

// readCreds resolves email and password from flags, then env, then an interactive prompt.
// Agent-driven onboarding passes --password / $MAGO_PASSWORD; a human gets a no-echo prompt.
func readCreds(rest []string, knownEmail string) (email, password string, err error) {
	email, password = knownEmail, ""
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--email":
			if i+1 < len(rest) {
				email = rest[i+1]
				i++
			}
		case "--password":
			if i+1 < len(rest) {
				password = rest[i+1]
				i++
			}
		}
	}
	if password == "" {
		password = os.Getenv("MAGO_PASSWORD")
	}
	if email == "" {
		email, err = prompt("Email: ", false)
		if err != nil {
			return
		}
	}
	if password == "" {
		password, err = prompt("Password: ", true)
		if err != nil {
			return
		}
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return "", "", fmt.Errorf("email and password are required")
	}
	return email, password, nil
}

// prompt reads a line from stdin; when hidden, it toggles terminal echo off via stty
// (stdlib-only — no x/term dependency).
func prompt(label string, hidden bool) (string, error) {
	fmt.Fprint(os.Stderr, label)
	if hidden {
		exec.Command("stty", "-echo").Run()
		defer func() {
			exec.Command("stty", "echo").Run()
			fmt.Fprintln(os.Stderr)
		}()
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line), err
}

// --- commands ---

func cmdRegister(args []string) error {
	cfg := loadConfig()
	email, password, err := readCreds(args, cfg.Email)
	if err != nil {
		return err
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := cfg.platformDo("POST", "/auth/signup", map[string]string{"email": email, "password": password}, false, &out); err != nil {
		return err
	}
	cfg.Email, cfg.Token = email, out.Token
	if err := cfg.save(); err != nil {
		return err
	}
	fmt.Printf("registered %s — token saved to %s\n", email, configPath())
	// Pull the account so the license is cached, and tailor the next step to the plan granted.
	acc, accErr := fetchAccount(cfg)
	onboard := func() {
		fmt.Println("next: install the mago GitHub App on your repos, then `mago link --installation <id>` to entitle them.")
		fmt.Println("      then `mago init ./company` and `mago serve --relay -C ./company` (run `mago worker doctor` first to check setup).")
	}
	switch {
	case accErr == nil && acc.Plan == "founding":
		fmt.Println("🏁 You're a FOUNDING operator — free during beta, with a direct line to the founder.")
		onboard()
	case accErr == nil && acc.Trial:
		fmt.Printf("✓ 48-hour free trial active (%s) — no card required.\n", trialRemaining(acc.TrialEnds))
		onboard()
		fmt.Println("      `mago subscribe` anytime to continue past the trial (€20/month).")
	default:
		fmt.Println("next: `mago subscribe` to activate the €20/month plan")
	}
	return nil
}

func cmdLogin(args []string) error {
	cfg := loadConfig()
	email, password, err := readCreds(args, cfg.Email)
	if err != nil {
		return err
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := cfg.platformDo("POST", "/auth/login", map[string]string{"email": email, "password": password}, false, &out); err != nil {
		return err
	}
	cfg.Email, cfg.Token = email, out.Token
	if err := cfg.save(); err != nil {
		return err
	}
	fmt.Printf("logged in as %s\n", email)
	return nil
}

func cmdSubscribe(args []string) error {
	cfg := loadConfig()
	var out struct {
		URL string `json:"url"`
	}
	if err := cfg.platformDo("POST", "/api/checkout", nil, true, &out); err != nil {
		return err
	}
	fmt.Println("Open this link to subscribe (€20/month):")
	fmt.Println()
	fmt.Println("  " + out.URL)
	fmt.Println()
	fmt.Println("After payment, run `mago account status` — the plan activates and a license key is issued.")
	return nil
}

// cmdBilling prints the Stripe customer-portal link so the human/CEO can manage the
// subscription (update card, download invoices, cancel). Requires a prior `mago subscribe`.
func cmdBilling(args []string) error {
	cfg := loadConfig()
	var out struct {
		URL string `json:"url"`
	}
	if err := cfg.platformDo("POST", "/api/portal", nil, true, &out); err != nil {
		return err
	}
	fmt.Println("Open this link to manage billing (card, invoices, cancel):")
	fmt.Println()
	fmt.Println("  " + out.URL)
	return nil
}

// cmdLink claims a GitHub App installation for the account (so the worker is entitled to
// receive that installation's repo events), or lists what's already linked.
func cmdLink(args []string) error {
	cfg := loadConfig()
	inst := ""
	list := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--installation", "-i":
			if i+1 < len(args) {
				inst = args[i+1]
				i++
			}
		case "list", "--list":
			list = true
		}
	}
	var out struct {
		Installations []struct {
			ID          int64    `json:"ID"`
			GithubLogin string   `json:"GithubLogin"`
			Repos       []string `json:"Repos"`
		} `json:"installations"`
		Repos []string `json:"repos"`
	}
	method, path := "GET", "/api/installations"
	var body any
	if !list && inst != "" {
		method = "POST"
		body = map[string]int64{"installation_id": atoiSafe64(inst)}
	} else if !list && inst == "" {
		list = true // bare `mago link` -> show current state + how to link (guide, don't error)
	}
	if err := cfg.platformDo(method, path, body, true, &out); err != nil {
		return err
	}
	if len(out.Installations) == 0 {
		fmt.Printf("no GitHub App installation linked yet.\n"+
			"  1. install the mago GitHub App on your repos (one click): %s/operators\n"+
			"  2. then: mago link --installation <id>   (the id is in the install URL: .../installations/<id>)\n",
			cfg.PlatformURL)
		return nil
	}
	for _, in := range out.Installations {
		fmt.Printf("installation %d (%s): %s\n", in.ID, orDefault(in.GithubLogin, "?"), strings.Join(in.Repos, ", "))
	}
	fmt.Printf("entitled repos: %s\n", strings.Join(out.Repos, ", "))
	if len(out.Repos) > 0 {
		fmt.Println("next: `mago init ./company`, then `mago serve --relay -C ./company` — agents act on issues labeled `mago`.")
	}
	return nil
}

func atoiSafe64(s string) int64 {
	var n int64
	for _, r := range strings.TrimSpace(s) {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

func cmdAccount(args []string) error {
	_, rest, err := parseCompanyDir(args)
	if err != nil {
		return err
	}
	if len(rest) == 0 || rest[0] != "status" {
		return fmt.Errorf("usage: mago account status")
	}
	cfg := loadConfig()
	out, err := fetchAccount(cfg)
	if err != nil {
		return err
	}
	status := "inactive"
	if out.Active {
		status = "active"
	}
	fmt.Printf("email:   %s\n", out.Email)
	fmt.Printf("plan:    %s (%s)\n", out.Plan, status)
	if out.Trial && out.TrialEnds > 0 {
		fmt.Printf("trial:   %s\n", trialRemaining(out.TrialEnds))
	}
	if out.LicenseKey != "" {
		fmt.Printf("license: %s\n", out.LicenseKey)
	} else {
		fmt.Println("license: (none yet — subscribe to activate)")
	}
	return nil
}

// accountInfo mirrors the platform's /api/account response.
type accountInfo struct {
	Email      string `json:"email"`
	Plan       string `json:"plan"`
	Active     bool   `json:"active"`
	Trial      bool   `json:"trial"`
	TrialEnds  int64  `json:"trial_ends"`
	LicenseKey string `json:"license_key"`
}

// fetchAccount GETs /api/account and caches the license key so the worker can authenticate.
func fetchAccount(cfg *cliConfig) (*accountInfo, error) {
	var out accountInfo
	if err := cfg.platformDo("GET", "/api/account", nil, true, &out); err != nil {
		return nil, err
	}
	if out.LicenseKey != "" && out.LicenseKey != cfg.LicenseKey {
		cfg.LicenseKey = out.LicenseKey
		cfg.save()
	}
	return &out, nil
}

// trialRemaining renders a human hint like "active, ~41h left" or "expired".
func trialRemaining(ends int64) string {
	d := time.Until(time.Unix(ends, 0))
	if d <= 0 {
		return "expired — run `mago subscribe` to continue"
	}
	h := int(d.Hours())
	if h >= 1 {
		return fmt.Sprintf("active, ~%dh left", h)
	}
	return fmt.Sprintf("active, ~%dm left", int(d.Minutes()))
}
