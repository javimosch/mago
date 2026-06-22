# Core vs platform

mago is **two Go modules in one repo**, split so the core can be open-sourced while the
operator's billing/secret logic stays private.

## core — `module mago` (repo root)

- `go 1.22`, **zero external dependencies** (stdlib only). `go list -m all` shows only `mago`.
- The `mago` client binary: the worker (agent runtime), the GitHub-native company, and a thin,
  secret-free HTTP **client** to the platform API (`account.go`: register/login/subscribe/account/
  link).
- Open-sourceable. The core never imports `platform/`; deleting `platform/` leaves a clean build.
- Build: `go build -o mago .` (from the repo root).

Key core files: `main.go` (command routing), `company.go` (company model + briefings),
`tick.go` (one agent tick), `route.go` (task→agent), `reconcile`/`serve.go` (event loop),
`relay.go` (worker dial-out to the platform), `review.go` (PR review), `git_state.go`
(mago-state branch), `tau.go` (agent harness driver), `github_backend.go` (issues↔tasks),
`account.go` (platform-API client), `writeback.go` (reflection → state/labels/journal).

## platform — `module mago-platform` (`platform/`, own `go.mod`)

- `go 1.25`, deps: `golang.org/x/crypto/bcrypt`, `modernc.org/sqlite` (pure-Go, no cgo).
- Operator-private control plane: accounts (bcrypt, JWT), Stripe billing, license issuance,
  GitHub App webhook relay, repo entitlement. Holds all secrets.
- Build from inside the dir: `cd platform && go build -o ../mago-platform .` (static deploy:
  `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`).

Key platform files: `main.go` (routes + daemon dispatch + `loadEnv`), `store.go` (SQLite),
`auth.go` (bcrypt + JWT + handlers), `stripe.go` (checkout + webhook), `relay.go` (worker stream
+ GitHub ingress + install registry + entitlement), `github.go` (operator-token webhook
provisioning), `setupgithub.go` (GitHub App manifest flow), `daemon.go` (start/stop/status).

## The rule

**Never add a third-party dependency to the core.** Use the stdlib, or put the code in
`platform/`. This is why, e.g., the worker↔platform relay is newline-delimited JSON over a
long-lived HTTP response instead of a WebSocket library, and why the core's JWT/HMAC/crypto is
hand-rolled on `crypto/*`.

See `docs/SAAS.md` §"Code organization" for the full rationale.
