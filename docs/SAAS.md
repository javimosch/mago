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

| Endpoint | Request | Response |
|---|---|---|
| `POST /auth/signup` | `{email, password}` | `{token}` (JWT) + sets account |
| `POST /auth/login` | `{email, password}` | `{token}` (JWT, HS256, ~30d) |
| `GET /api/account` | `Authorization: Bearer <jwt>` | `{email, plan, active, license_key?}` |
| `POST /api/checkout` | `Bearer <jwt>` | `{url}` — Stripe Checkout link (subscription mode, the €20 price, idempotency key) |
| `POST /stripe/webhook` | Stripe-signed body | 200; `checkout.session.completed` → `plan=mago` + issue `license_key`; `subscription.deleted` → `free` |
| `WSS /ws/worker?token=<license_key>` | — | event stream (relayed GitHub webhooks); rejected if subscription inactive |
| `POST /webhooks/github/<install>` | GitHub-signed body | 200; relayed to the matching worker |

The CLI stores `{platform_url, token, license_key}` in `~/.mago/config.json` (0600), rcmd-style.

## Client CLI (the `mago` binary)

- `mago register --email …` / `mago login` → the platform auth API; token in `~/.mago/config.json`
- `mago subscribe` → prints the Stripe checkout link; on success the account goes active
- `mago account status`
- the worker connects to the platform with its license key and receives relayed GitHub webhooks

## Worker ↔ platform: the webhook relay

The worker runs on the client's machine (often behind NAT), so it **dials out** to the
platform and the platform pushes events down — exactly AM's pattern, but carrying GitHub
webhook events instead of run commands:

1. Worker connects: `WSS /ws/worker?token=<license_key>` → platform validates the license
   (active subscription) and records `worker_links(license_key, worker_id, repos…)`.
2. The platform owns **one GitHub App / webhook ingress**. A repo event (`issues`,
   `issue_comment`, `pull_request`) arrives at `POST /webhooks/github/<install>`.
3. The platform looks up which worker serves that repo and pushes the event down the socket.
4. The worker feeds it into the existing `mago serve` event loop (`classifyEvent` → wake →
   reconcile / targeted agent / reviewPR). **This removes the per-worker tunnel** that the
   current `mago serve` needs.

License-gating: if the subscription lapses (`subscription.deleted` → plan `free`), the
platform refuses the worker's connection, so an unpaid worker stops receiving events.

## Onboarding (agent-driven — mago's twist on AM)

The human's own agent (e.g. Claude Code) drives the `mago` CLI; the human just approves:

```
mago register --email you@co.com        # POST /auth/signup -> token in ~/.mago/config.json
mago subscribe                          # POST /api/checkout -> prints the Stripe link; pay;
                                        #   webhook marks active + issues the license key
mago account status                     # GET /api/account -> { plan: "mago", active: true }
mago worker add <name>                  # registers a worker; pulls the license key
mago worker doctor                      # checks tau + gh are configured
mago company create acme                # worker's gh creates the repo, scaffolds .mago/,
                                        #   cuts mago-state, installs the relay webhook, seeds the exec team
# from here: the human is CEO — files issues, answers HITL on GitHub; the exec team runs it
```

The whole loop is CLI + GitHub — no web panel. The human is the **CEO**; the seeded exec team
(CTO/CMO/Head of Product/Head of Org Engineering) runs the company.

## Build roadmap

1. **Now** — Stripe €20/mo price created ✓ + this plan.
2. **Platform backend skeleton** ✓ (`mago-platform`): accounts (email/pw, JSON store, JWT) +
   Stripe checkout + webhook + license issuance, mirroring AM's `stripe.go` simplified to one
   plan. Stdlib-only skeleton; prod swaps bcrypt + SQLite behind the same method set.
3. **Client CLI** ✓: `register`/`login`/`subscribe`/`account status` (`account.go`) — a thin,
   secret-free HTTP client to the platform API; token + license cached in `~/.mago/config.json`.
   Verified end-to-end against real Stripe test mode. Worker license-gating is phase 4.
4. **GitHub webhook relay**: the platform receives repo webhooks and relays them to the worker
   over its dial-out connection — so `mago serve` no longer needs a public URL/tunnel. The
   worker authenticates with the cached `license_key`; lapsed subscriptions are refused.
5. **v2**: GitHub App + `gh` login; live Stripe price; multiple workers per account.

## Code organization — core vs platform (decided)

One repo, but cleanly modular so the **mago core can be open-sourced later** while the
platform layer stays private:

- **`mago` core (root package) — the open-source candidate.** The CLI, the worker, the agent
  runtime, and the *thin* platform-API **client** commands (`register`/`login`/`subscribe`/
  `account status`). These hold NO operator secrets — they're just an HTTP client to the
  platform API — so they're safe to make public.
- **`platform/` package — operator-private.** The `mago-platform` binary: accounts, Stripe,
  license issuance, the GitHub webhook relay. **All** billing/secret logic lives here and
  nowhere else. The core never imports `platform/`.
- **Build:** `go build -o mago .` (core) and `go build -o mago-platform ./platform` (private).
- **To open-source:** publish the root and keep `platform/` in a private overlay/submodule.
  A small `platformclient/` package (HTTP client to the platform API, no secrets) can stay in
  core so the client commands compile without `platform/`.

Still open (not blocking): reuse AM's Stripe account for **production** vs a dedicated mago
account (test reuse is fine now); the worker↔platform transport (lean toward AM's WebSocket
dial-out — see below).

## Data model (platform SQLite, `~/.mago-platform/platform.db`)

```sql
users (
  id            INTEGER PRIMARY KEY,
  email         TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,              -- bcrypt cost 12
  plan          TEXT NOT NULL DEFAULT 'free',  -- 'free' | 'mago' (the €20 plan)
  stripe_customer TEXT,
  stripe_sub      TEXT,
  license_key   TEXT UNIQUE,               -- mago_<48hex>, issued on first payment
  created_at    INTEGER NOT NULL
)
stripe_events ( id TEXT PRIMARY KEY, type TEXT, seen_at INTEGER )  -- webhook dedup
worker_links ( license_key TEXT, worker_id TEXT, repo TEXT, connected_at INTEGER )  -- which worker relays for which repo
```

No repos/runs/workers-pool tables (the client's GitHub repo is the company; the worker
journals to `mago-state`). Only what billing + relay routing need.
