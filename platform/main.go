// Command mago-platform is the operator-private control plane: accounts, Stripe billing,
// license issuance (and, later, the GitHub webhook relay). It is a separate binary from the
// `mago` core (which never imports this package), so the core can be open-sourced while this
// stays private. Build: `go build -o mago-platform ./platform`. See docs/SAAS.md.
package main

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type server struct {
	store                                                *Store
	jwtSecret, stripeKey, webhookSecret, priceID, appURL string
}

func main() {
	loadDotenv("platform/.env")
	port := env("PORT", "9100")
	dbPath := expand(env("DB_PATH", "~/.mago-platform/store.json"))
	st, err := openStore(dbPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	s := &server{
		store:         st,
		jwtSecret:     env("JWT_SECRET", "dev-insecure-change-me"),
		stripeKey:     os.Getenv("STRIPE_SECRET_KEY"),
		webhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		priceID:       os.Getenv("STRIPE_PRICE_MAGO"),
		appURL:        env("APP_URL", "http://localhost:"+port),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok\n") })
	mux.HandleFunc("/auth/signup", s.handleSignup)
	mux.HandleFunc("/auth/login", s.handleLogin)
	mux.HandleFunc("/api/account", s.handleAccount)
	mux.HandleFunc("/api/checkout", s.handleCheckout)
	mux.HandleFunc("/stripe/webhook", s.handleWebhook)
	// TODO(phase 4): WSS /ws/worker (license-gated) + POST /webhooks/github/<install> relay.

	log.Printf("mago-platform :%s  store=%s  stripe=%v price=%s", port, dbPath, s.stripeKey != "", s.priceID)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
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
		if os.Getenv(k) == "" {
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
