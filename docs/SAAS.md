# SaaS — the mago platform

How mago becomes a product. It reuses AutoMaintainer's proven billing/auth/license layer
but keeps mago's own direction (set in [VISION.md](VISION.md) / [ARCHITECTURE.md](ARCHITECTURE.md)):
**BYOK, CLI-only, a single €20/month plan, and a GitHub-webhook relay to NAT'd workers.**

## The model (mago's direction — unchanged)

- **BYOK + workers.** The client's LLM key stays on their worker; mago never sells
  completions. Compute runs on the client's machine.
- **CLI-only.** No web dashboard. Onboarding is **agent-driven** — the human's own agent
  (e.g. Claude Code) drives the `mago` CLI. The company lives in a GitHub repo the client owns.
- **Single plan: €20/month.** No free tier, no Pro/Max tiers, no per-resource limits.
- **Two binaries** (build-tag split, like AM's `am`/`am-cloud`): `mago` (client = CLI +
  worker, public) and `mago-platform` (operator-private = accounts + Stripe + webhook relay).
- The platform is a thin control plane; the worker + the client's GitHub repo are the company.

## Reuse from AutoMaintainer (battle-tested)

AM's panel (`~/ai/automaintainer-saas-panel`, Go + SQLite) gives us a clean pattern to mirror:

- **Stripe checkout** — `session.New` in `subscription` mode: get/create customer, one line
  item (the price), metadata (`user_id`, `plan`), idempotency key, success/cancel URLs.
- **Stripe webhook** — `webhook.ConstructEventWithOptions` (sig verify) + **event dedup**
  (`INSERT OR IGNORE` on a `stripe_events` table) + on `checkout.session.completed` → set the
  user active and **issue a license key**; on `subscription.updated/deleted` → activate/downgrade.
- **License key** — `generateLicenseKey()` → `mago_<48 hex>`; validated when the worker connects.
- **Accounts** — `users(id, email, password_hash[bcrypt cost 12], plan, stripe_customer,
  stripe_sub, license_key, api_key)`; JWT (HS256) for the CLI session.

## Drop (mago is leaner than AM)

Web dashboard · worker pool / WebSocket fleet (mago = one worker) · the scheduler (the worker
ticks autonomously) · plan tiers & per-resource limits (one flat plan) · repo CRUD (the
client's GitHub repo *is* the company) · the run table (the worker journals to `mago-state`) ·
GitHub OAuth at signup (email/password v1; the client's `gh`/PAT lives in their worker).

## Stripe (test mode — reusing the AM account for now)

Created via the Stripe API in **test mode** (`livemode=false`), reusing AM's `sk_test_` key:

| | |
|---|---|
| Product | `prod_UkM5Xqed6N3jNd` — "mago" |
| Price | `price_1TkrN64Gw2MGvAdPtEmGHB0o` — **€20.00/month** recurring |

Platform env vars (see `platform/.env.example`):
`STRIPE_SECRET_KEY` (sk_test_… from AM's `.env`), `STRIPE_WEBHOOK_SECRET`,
`STRIPE_PRICE_MAGO=price_1TkrN64Gw2MGvAdPtEmGHB0o`, `APP_URL`, `JWT_SECRET`.
A **live** price (`sk_live_`) is created later for production.

## Platform API surface (driven by the CLI, no UI)

- `POST /auth/signup` (email, password) → account
- `POST /auth/login` (email, password) → JWT session token
- `POST /api/checkout` → Stripe Checkout session URL (the `mago subscribe` CLI prints it)
- `GET  /api/account` → plan + subscription status (`mago account status`)
- `POST /stripe/webhook` → on `checkout.session.completed`: mark active + issue license key
- `POST /api/worker/register` (license key) → returns platform config (the webhook-relay endpoint)
- `POST /webhooks/github/<user>` → GitHub repo event → **relay down to that user's worker**
  (the worker dials out, so NAT is irrelevant)

## Client CLI (the `mago` binary)

- `mago register --email …` / `mago login` → the platform auth API; token in `~/.mago/config.json`
- `mago subscribe` → prints the Stripe checkout link; on success the account goes active
- `mago account status`
- the worker connects to the platform with its license key and receives relayed GitHub webhooks

## Onboarding (agent-driven — mago's twist on AM)

The human's own agent runs the CLI for them: `register → subscribe` (open the Stripe link) →
`worker add` → `company create` (in the client's GitHub). The human is the **CEO**; the seeded
exec team runs the company. The whole loop happens through the CLI + GitHub — no panel.

## Build roadmap

1. **Now** — Stripe €20/mo price created ✓ + this plan.
2. **Platform backend skeleton** (`mago-platform`): accounts (email/pw, SQLite, bcrypt, JWT) +
   Stripe checkout + webhook + license issuance, mirroring AM's `stripe.go` simplified to one plan.
3. **Client CLI**: `register`/`login`/`subscribe`/`account status`; the worker is license-gated.
4. **GitHub webhook relay**: the platform receives repo webhooks and relays them to the worker
   over its dial-out connection — so `mago serve` no longer needs a public URL/tunnel.
5. **v2**: GitHub App + `gh` login; live Stripe price; multiple workers per account.

## Open decisions (to steer the build)

- **Platform repo** — a separate `mago-platform` repo (like AM's panel) vs in-`mago` behind
  build tags. AM uses a separate repo; the design calls for a private operator binary.
- **Stripe account** — reuse AM's account for production too, or a dedicated mago account.
- **Worker↔platform transport** — reuse AM's WebSocket dial-out, or a long-poll/SSE.
