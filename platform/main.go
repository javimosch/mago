// Command mago-platform is the operator-private control plane: accounts, Stripe billing,
// license issuance (and, later, the GitHub webhook relay). It is a separate binary from the
// `mago` core (which never imports this package), so the core can be open-sourced while this
// stays private. Build: `go build -o mago-platform ./platform`. See docs/SAAS.md.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type server struct {
	store                                                                          *Store
	jwtSecret, stripeKey, webhookSecret, priceID, appURL, ghWebhookSecret, ghAppID string
	enforceEntitlement                                                             bool
	hub                                                                            *relayHub
}

func main() {
	loadEnv()
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "webhook": // provision the GitHub webhook ingress with the operator token
			fail(cmdWebhook(os.Args[2:]))
			return
		case "start": // run the server (hotify/daemon entrypoint): `mago-platform start --port N`
			fail(cmdStart(os.Args[2:]))
			return
		case "stop":
			fail(cmdStop())
			return
		case "restart": // stop the actual port listener (not just the pidfile pid) + start daemonized
			fail(cmdRestart(os.Args[2:]))
			return
		case "status":
			fail(cmdStatus())
			return
		case "activity": // onboarding observability: signups, subs, worker connects
			fail(cmdActivity(os.Args[2:]))
			return
		case "usage": // adoption depth: relayed GitHub activity per account
			fail(cmdUsage(os.Args[2:]))
			return
		case "setup-github": // create the GitHub App via the manifest flow (one browser click)
			fail(cmdSetupGithub(os.Args[2:]))
			return
		case "reset-password": // operator password recovery; local DB access only, no HTTP surface
			fail(cmdResetPassword(os.Args[2:]))
			return
		case "serve": // cli-daemon-spec §1: foreground primitive, --host/--port, loopback default
			fail(cmdServe(os.Args[2:]))
			return
		case "daemon": // cli-daemon-spec §4: idempotent start|stop|status over /_health + /_shutdown
			fail(cmdDaemon(os.Args[2:]))
			return
		case "help-json", "--help-json": // cli-output-spec §4: machine-readable command catalog
			fail(cmdHelpJSON())
			return
		}
	}
	runServer(env("MAGO_PLATFORM_HOST", "127.0.0.1"), env("PORT", "9100")) // no-arg: run in foreground (dev convenience)
}

func fail(err error) {
	if err != nil {
		log.Fatalf("error: %v", err)
	}
}

// handleSubscribed is the Stripe checkout success/cancel landing page. Onboarding is CLI-driven,
// so it just tells the human to return to the terminal.
func handleSubscribed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	msg := "✓ Subscription active. Return to your terminal and run <code>mago account status</code>."
	if r.URL.Query().Get("cancelled") != "" {
		msg = "Checkout cancelled. Run <code>mago subscribe</code> to try again."
	}
	fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><title>mago</title>`+
		`<body style="font-family:system-ui,sans-serif;max-width:34rem;margin:5rem auto;text-align:center;color:#222">`+
		`<h2>mago</h2><p style="font-size:1.1rem">%s</p></body>`, msg)
}

// runServer boots the store, wires routes, and serves until killed. Blocks. host defaults to
// loopback everywhere it's called from (cli-daemon-spec §6) -- Traefik already reaches this
// process via 127.0.0.1 in the dk1 deploy, so this closes the all-interfaces exposure with no
// behavior change for the existing deployment.
func runServer(host, port string) {
	dbPath := expand(env("DB_PATH", "~/.mago-platform/platform.db"))
	st, err := openStore(dbPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()
	s := &server{
		store:           st,
		jwtSecret:       env("JWT_SECRET", "dev-insecure-change-me"),
		stripeKey:       strings.TrimSpace(os.Getenv("STRIPE_SECRET_KEY")),
		webhookSecret:   strings.TrimSpace(os.Getenv("STRIPE_WEBHOOK_SECRET")),
		priceID:         strings.TrimSpace(os.Getenv("STRIPE_PRICE_MAGO")),
		appURL:          env("APP_URL", "http://localhost:"+port),
		ghWebhookSecret: strings.TrimSpace(os.Getenv("GITHUB_WEBHOOK_SECRET")),
		ghAppID:         strings.TrimSpace(os.Getenv("GITHUB_APP_ID")),
		hub:             newRelayHub(),
	}
	// Enforce repo entitlement whenever we're multi-tenant: a GitHub App is configured, or the
	// operator opts in (webhook-provisioned path). Off by default for local/single-tenant dev.
	s.enforceEntitlement = s.ghAppID != "" || strings.TrimSpace(os.Getenv("GITHUB_ENFORCE_ENTITLEMENT")) == "1"

	shutdownToken, err := writeShutdownToken()
	if err != nil {
		log.Fatalf("shutdown token: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok\n") }) // legacy, kept for existing deploy verify
	mux.HandleFunc("/_health", handleHealth)                                                               // cli-daemon-spec §2
	mux.HandleFunc("/_shutdown", handleShutdown(host, shutdownToken))                                      // cli-daemon-spec §3
	mux.HandleFunc("/", s.handleLanding)                                                                   // public landing (also catches unmatched -> 404)
	mux.HandleFunc("/install.sh", s.handleInstall)
	mux.HandleFunc("/dl/mago", s.handleDownload) // prebuilt CLI binary
	mux.HandleFunc("/version", s.handleVersion)  // cli-update-spec §2: what /dl/mago currently serves
	mux.HandleFunc("/operators", s.handleOperators)
	mux.HandleFunc("/llms.txt", s.handleLLMs)       // agent-readable onboarding (the operator "skill")
	mux.HandleFunc("/subscribed", handleSubscribed) // Stripe success/cancel landing (CLI onboarding)
	mux.HandleFunc("/auth/signup", s.handleSignup)
	mux.HandleFunc("/auth/login", s.handleLogin)
	mux.HandleFunc("/api/account", s.handleAccount)
	mux.HandleFunc("/api/checkout", s.handleCheckout)
	mux.HandleFunc("/api/portal", s.handlePortal)                // `mago billing`: Stripe customer portal link
	mux.HandleFunc("/api/installations", s.handleInstallations)  // `mago link`: claim/list App installs
	mux.HandleFunc("/api/worker/control", s.handleWorkerControl) // `mago worker mode`: switch a worker live
	mux.HandleFunc("/api/feedback", s.handleFeedback)            // `mago feedback`: operator friction/bugs/requests
	mux.HandleFunc("/api/usage", s.handleUsage)                  // adoption-depth feed (planner sense() input)
	mux.HandleFunc("/stripe/webhook", s.handleWebhook)
	mux.HandleFunc("/ws/worker", s.handleWorkerStream)         // worker dial-out (license-gated)
	mux.HandleFunc("/webhooks/github/", s.handleGithubWebhook) // GitHub App ingress -> relay

	addr := net.JoinHostPort(host, port)
	log.Printf("mago-platform %s  store=%s  stripe=%v price=%s gh-relay=%v gh-app=%v entitle=%v", addr, dbPath, s.stripeKey != "", s.priceID, s.ghWebhookSecret != "", s.ghAppID != "", s.enforceEntitlement)
	fmt.Fprintf(os.Stderr, "[serve] listening on http://%s\n", addr) // cli-daemon-spec §1: flushed before the accept loop
	log.Fatal(http.ListenAndServe(addr, mux))
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// loadEnv loads .env from (in priority order) $MAGO_PLATFORM_ENV, the repo dev path, the
// directory of the executable (how it's found in a hotify deploy), and the cwd. Earlier files
// win since loadDotenv never overrides an already-set var.
func loadEnv() {
	if p := strings.TrimSpace(os.Getenv("MAGO_PLATFORM_ENV")); p != "" {
		loadDotenv(expand(p))
	}
	loadDotenv("platform/.env") // dev: run from the repo root
	if exe, err := os.Executable(); err == nil {
		loadDotenv(filepath.Join(filepath.Dir(exe), ".env")) // deployed: .env beside the binary
	}
	loadDotenv(".env")
}

// loadDotenv sets env vars from a KEY=VALUE file, without overriding existing env.
func loadDotenv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if strings.TrimSpace(os.Getenv(k)) == "" {
			os.Setenv(k, v)
		}
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(v); err != nil {
		httpErr(w, 400, "invalid json")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func atoi(s string) int64 { n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64); return n }
