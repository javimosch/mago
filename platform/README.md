# mago-platform (operator-private)

The control plane behind mago: **accounts, Stripe billing, license issuance** (and, later, the
GitHub webhook relay). A separate binary from the `mago` core — the core never imports this
package, so the core stays open-sourceable while billing/secret logic lives only here.

See [`../docs/SAAS.md`](../docs/SAAS.md) for the full design (the single €20/mo plan, BYOK +
workers, CLI-only onboarding, the webhook relay).

## Run

```sh
cp platform/.env.example platform/.env   # fill in secrets (gitignored — never commit)
go build -o mago-platform ./platform
./mago-platform                          # reads platform/.env, listens on $PORT (default 9100)
```

`.env` keys: `APP_URL`, `PORT`, `JWT_SECRET`, `STRIPE_SECRET_KEY` (sk_test_… reused from AM for
now), `STRIPE_WEBHOOK_SECRET`, `STRIPE_PRICE_MAGO`, `DB_PATH`.

## Endpoints

| Endpoint | Auth | Purpose |
|---|---|---|
| `GET /healthz` | — | liveness |
| `POST /auth/signup` | — | `{email,password}` → `{token}`; creates a `free` account |
| `POST /auth/login` | — | `{email,password}` → `{token}` (JWT, HS256, ~30d) |
| `GET /api/account` | Bearer | `{email, plan, active, license_key}` |
| `POST /api/checkout` | Bearer | → `{url}` Stripe Checkout link (subscription mode, the €20 price) |
| `POST /stripe/webhook` | Stripe sig | `checkout.session.completed` → `plan=mago` + issue license; `customer.subscription.deleted` → `free` |

`TODO(phase 4)`: `WSS /ws/worker` (license-gated) + `POST /webhooks/github/<install>` relay.

## Skeleton notes (intentional, see docs/SAAS.md §roadmap)

- **Store** is a JSON file (`store.go`) behind locked methods. Production swaps it for SQLite
  (schema in `docs/SAAS.md`) behind the same method set — handlers don't change.
- **Passwords** use a placeholder stdlib salted-iterated-SHA-256 KDF so the skeleton stays
  stdlib-only. Production must switch to bcrypt/argon2 (`golang.org/x/crypto`).
- **Stripe** is called over raw `net/http` (`SetBasicAuth(key,"")`) instead of `stripe-go`, and
  the webhook signature is verified by hand (`verifyStripeSig`) — keeps the skeleton dependency-free.

## Smoke test

```sh
# signup → checkout returns a real test session URL; a signed webhook activates + issues a license
curl -sX POST localhost:9100/auth/signup -d '{"email":"a@b.c","password":"hunter2pass"}'
# (sign a checkout.session.completed body with STRIPE_WEBHOOK_SECRET, POST to /stripe/webhook)
```

Verified locally: `free → checkout → activate (mago + license) → downgrade (free)`, plus event
dedup and bad-signature rejection.
