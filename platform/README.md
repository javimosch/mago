# mago-platform (operator-private)

The control plane behind mago: **accounts, Stripe billing, license issuance, GitHub webhook
relay**. A separate binary **and a separate Go module** from the `mago` core — the core never
imports this and stays zero-dependency (stdlib-only, `go 1.22`), while this module carries the
billing/secret logic and its deps (bcrypt, SQLite). That makes the core safe to open-source
and this directory a drop-in private overlay.

See [`../docs/SAAS.md`](../docs/SAAS.md) for the full design (the single €20/mo plan, BYOK +
workers, CLI-only onboarding, the webhook relay).

## Run

This is its own module, so build from inside `platform/`:

```sh
cp platform/.env.example platform/.env       # fill in secrets (gitignored — never commit)
cd platform && go build -o ../mago-platform . # nested module — build here, not from the root
cd .. && ./mago-platform                      # reads platform/.env, listens on $PORT (default 9100)
```

`.env` keys: `APP_URL`, `PORT`, `JWT_SECRET`, `STRIPE_SECRET_KEY` (sk_test_… reused from AM for
now), `STRIPE_WEBHOOK_SECRET`, `STRIPE_PRICE_MAGO`, `GITHUB_WEBHOOK_SECRET`, `DB_PATH`
(default `~/.mago-platform/platform.db`).

## Endpoints

| Endpoint | Auth | Purpose |
|---|---|---|
| `GET /healthz` | — | liveness |
| `POST /auth/signup` | — | `{email,password}` → `{token}`; creates a `free` account |
| `POST /auth/login` | — | `{email,password}` → `{token}` (JWT, HS256, ~30d) |
| `GET /api/account` | Bearer | `{email, plan, active, license_key}` |
| `POST /api/checkout` | Bearer | → `{url}` Stripe Checkout link (subscription mode, the €20 price) |
| `GET/POST /api/installations` | Bearer | list / claim (`mago link`) GitHub App installs → entitled repos |
| `POST /stripe/webhook` | Stripe sig | `checkout.session.completed` → `plan=mago` + issue license; `customer.subscription.deleted` → `free` |
| `GET /ws/worker?token&repos` | license | NDJSON stream of relayed GitHub events (`401` unknown, `403` lapsed); claimed repos intersected with the account's entitled repos when `GITHUB_APP_ID` is set |
| `POST /webhooks/github/<install>` | GitHub sig | `ping`→ack; `installation`/`installation_repositories`→update registry; else relay to the worker serving that repo |

## Implementation notes

- **Store** is SQLite (`store.go`, pure-Go `modernc.org/sqlite` driver — no cgo). `Update(id,
  fn)` loads a user row, applies `fn`, and writes it back in a transaction; `FirstEvent` dedups
  webhooks with `INSERT OR IGNORE`.
- **Passwords** use **bcrypt** (cost 12, `golang.org/x/crypto/bcrypt`).
- **Stripe** is called over raw `net/http` (`SetBasicAuth(key,"")`) instead of `stripe-go`, and
  the webhook signature is verified by hand (`verifyStripeSig`) — fewer dependencies.
- **Relay** (`relay.go`) keeps an in-memory `license → connection` hub; persisting `worker_links`
  is a v2 nicety (one worker per account today).
- **GitHub App** (`relay.go` + `store.go`): signed `installation`/`installation_repositories`
  webhooks maintain the `installations` table (GitHub-authoritative repo lists); `mago link`
  binds an installation to an account; worker repo subscriptions are entitlement-checked against
  it when `GITHUB_APP_ID` is set. See `docs/SAAS.md` §"The GitHub App" for the manual setup.
- **No-App path** (`github.go`): the operator token can provision the ingress webhook directly —
  no GitHub App, no browser click. Entitlement (when `GITHUB_ENFORCE_ENTITLEMENT=1`) comes from
  `repo_grants`:
  ```sh
  mago-platform webhook add  --repo owner/repo --url https://<public-host> --account user@co.com
  mago-platform webhook list --repo owner/repo
  mago-platform webhook rm   --repo owner/repo --id <hookID>
  ```
  Needs a token with `admin:repo_hook` (or `admin:org_hook` for `--org`) and a public host where
  GitHub can reach `…/webhooks/github/`.

## Smoke test

```sh
# signup → checkout returns a real test session URL; a signed webhook activates + issues a license
curl -sX POST localhost:9100/auth/signup -d '{"email":"a@b.c","password":"hunter2pass"}'
# (sign a checkout.session.completed body with STRIPE_WEBHOOK_SECRET, POST to /stripe/webhook)
```

Verified locally on SQLite + bcrypt: `free → checkout → activate (mago + license) → downgrade
(free)`, plus event dedup, bad-signature rejection, and the relay (connect → routed webhook →
worker, with unknown/lapsed licenses refused).
