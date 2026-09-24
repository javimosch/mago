# Core vs platform

mago is **two Go modules in two repos**, split so the core can be open-sourced while the
operator's billing/secret logic stays private.

## core — `module mago` (this repo, public, Apache-2.0)

- `go 1.22`, **zero external dependencies** (stdlib only). `go list -m all` shows only `mago`.
- The `mago` client binary: the worker (agent runtime), the GitHub-native company, and a thin,
  secret-free HTTP **client** to the platform API (`account.go`: register/login/subscribe/account/
  link). That client is optional — every local and GitHub-webhook path works without it.
- Build: `go build -o mago .` (from the repo root).

Key core files: `main.go` (command routing), `company.go` (company model + briefings),
`tick.go` (one agent tick), `route.go` (task→agent), `reconcile`/`serve.go` (event loop),
`relay.go` (worker dial-out to the platform), `review.go` (PR review), `git_state.go`
(mago-state branch), `tau.go` (agent harness driver), `github_backend.go` (issues↔tasks),
`account.go` (platform-API client), `writeback.go` (reflection → state/labels/journal).

## platform — `module mago-platform` (private repo `javimosch/mago-platform`)

- `go 1.25`, deps: `golang.org/x/crypto/bcrypt`, `modernc.org/sqlite` (pure-Go, no cgo).
- Operator-private control plane: accounts (bcrypt, JWT), Stripe billing, license issuance,
  GitHub App webhook relay, repo entitlement, the marketing site. Holds all secrets.
- Lives in its own repository and is **not** required to build, test or run the core. If you
  are reading this from the public repo, you will not have it, and nothing here needs it.

## The rule

**Never add a third-party dependency to the core.** Use the stdlib, or put the code in the
platform repo. This is why, e.g., the worker↔platform relay is newline-delimited JSON over a
long-lived HTTP response instead of a WebSocket library, and why the core's JWT/HMAC/crypto is
hand-rolled on `crypto/*`.
