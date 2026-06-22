# SaaS platform

`mago-platform` is the operator-private control plane. Model: **BYOK + workers** (like
AutoMaintainer) — mago sells no completions; the client's LLM key + compute stay on their
worker. **CLI-only**, no web panel. Single **€20/month** plan. Full design: `docs/SAAS.md`.

## Live deployment

Running at **https://mago.intrane.fr** on the **dk1** VM (`vpspoly1`) behind Traefik (TLS) →
`localhost:9100`. Operational runbook + redeploy steps: `docs/DEPLOY.md`. Stripe is **LIVE**
(reuses AutoMaintainer's `sk_live_`; dedicated mago product/price/webhook). Signup grants a **48h
no-card trial** (license issued immediately); `mago subscribe` converts to the €20/mo plan and
`mago billing` opens the Stripe customer portal. Public surface: landing `/`, installer
`/install.sh`, multi-arch binary `/dl/mago`, human guide `/operators`, agent guide `/llms.txt`.
Operator metrics: `mago-platform activity` (see [metrics.md](metrics.md)).

## Endpoints (driven by the CLI, no UI)

| Endpoint | Purpose |
|---|---|
| `POST /auth/signup`, `/auth/login` | accounts → JWT (HS256) |
| `GET /api/account` | plan + license |
| `POST /api/checkout` | Stripe Checkout link (subscription, the €20 price) — `mago subscribe` |
| `POST /api/portal` | Stripe customer-portal link (manage/cancel) — `mago billing` |
| `GET/POST /api/installations` | list / claim GitHub App installs (`mago link`) |
| `POST /stripe/webhook` | `checkout.session.completed`→`plan=mago`+issue license; `subscription.deleted`→`free` |
| `GET /ws/worker?token&repos` | worker dial-out: NDJSON stream of relayed GitHub events (license-gated) |
| `POST /webhooks/github/<install>` | GitHub App ingress → verify sig → relay to the worker |

## License & activation

Signup issues a license `mago_<48hex>` immediately and starts a **48h trial** (`plan=trial`,
`trial_ends`); first payment flips `plan=mago` via the Stripe webhook. The worker authenticates to
the relay with the license; `entitled()` = `plan=mago` OR live trial. Unknown license → `401`;
lapsed sub or expired trial → `403`. Store is SQLite (`platform.db`); passwords are bcrypt cost 12.
Onboarding events (signup/subscribed/worker_connect/…) are recorded in an `events` table — see
[metrics.md](metrics.md).

## The webhook relay (removes the per-worker tunnel)

The worker is behind NAT, so it **dials out**: `mago serve --relay` → `GET /ws/worker` and holds
a long-lived NDJSON stream (stdlib, not WebSocket). The platform owns one **GitHub App** ingress;
a repo event arrives at `/webhooks/github/`, signature is verified against `GITHUB_WEBHOOK_SECRET`,
and it's streamed to the worker serving that repo, which feeds it through the same
`classifyEvent` path the local listener uses.

## Repo entitlement (multi-tenant safety)

Workers self-declare `?repos=`, so when `GITHUB_APP_ID` (or `GITHUB_ENFORCE_ENTITLEMENT=1`) is
set, the platform intersects a worker's claimed repos with its account's **entitled** repos —
the union of repos across the GitHub App installations it has claimed (`installation` /
`installation_repositories` webhooks populate the registry; `mago link` binds an installation to
an account) plus operator `repo_grants`. A worker can't receive another tenant's events by naming
their repo.

## Wiring GitHub to the platform

Two paths (see `docs/SAAS.md` §"The GitHub App"):
1. **GitHub App** (multi-tenant, self-serve): `mago-platform setup-github --url https://<host>`
   creates the App via the manifest flow (one browser click) and writes its id/secret/key; the
   customer installs it; `mago link` claims the installation.
2. **Operator-token webhooks** (operator-run, zero clicks): `mago-platform webhook add --repo
   owner/repo --url https://<host> --account <email>` provisions a repo/org webhook via the
   operator's token and grants entitlement. Good for bootstrapping; the App is the product path.
