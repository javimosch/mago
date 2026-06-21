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
| `GET /api/installations` | `Bearer <jwt>` | `{installations[], repos[]}` — claimed GitHub App installs + entitled repos |
| `POST /api/installations` | `Bearer <jwt>` `{installation_id}` | claims an installation for the account (`mago link`) |
| `POST /stripe/webhook` | Stripe-signed body | 200; `checkout.session.completed` → `plan=mago` + issue `license_key`; `subscription.deleted` → `free` |
| `GET /ws/worker?token=<license_key>&repos=…` | — | long-lived NDJSON stream of relayed GitHub webhooks; `401` unknown license, `403` if subscription inactive. Claimed repos are intersected with the account's **entitled** repos (see GitHub App below) |
| `POST /webhooks/github/<install>` | GitHub-signed body | 200; `ping`→ack, `installation`/`installation_repositories`→update the install registry, else relayed to the worker serving that repo |

**Relay transport:** newline-delimited JSON over a long-lived HTTP response (the worker `GET`s
and reads a stream), **not** raw WebSocket — keeps the core stdlib-only. The worker dials out,
so it works behind NAT exactly like a WebSocket would, at the low volume GitHub webhooks need.

The CLI stores `{platform_url, token, license_key}` in `~/.mago/config.json` (0600), rcmd-style.

## Client CLI (the `mago` binary)

- `mago register --email …` / `mago login` → the platform auth API; token in `~/.mago/config.json`
- `mago subscribe` → prints the Stripe checkout link; on success the account goes active
- `mago account status`
- `mago link --installation <id>` / `mago link list` → claim a GitHub App installation so the
  account's repos are entitled (the worker then receives those repos' relayed events)
- the worker connects to the platform with its license key and receives relayed GitHub webhooks

## Worker ↔ platform: the webhook relay

The worker runs on the client's machine (often behind NAT), so it **dials out** to the
platform and the platform pushes events down — exactly AM's pattern, but carrying GitHub
webhook events instead of run commands. **Implemented** (`platform/relay.go`, `relay.go`):

1. Worker connects: `mago serve --relay` → `GET /ws/worker?token=<license_key>&repos=…`
   (repos = the company repo + project repos). The platform validates the license (active
   subscription) and registers the connection in an in-memory hub keyed by license.
2. The platform owns **one GitHub App / webhook ingress**. A repo event (`issues`,
   `issue_comment`, `pull_request`) arrives at `POST /webhooks/github/<install>`; the platform
   verifies `X-Hub-Signature-256` against `GITHUB_WEBHOOK_SECRET`.
3. The platform looks up which worker serves `repository.full_name` and streams the event
   (`{event, body}` as one NDJSON line) down that connection; keepalive `ping`s every 25s.
4. The worker feeds `body` into the existing event loop via the SAME `classifyEvent` → wake →
   reconcile / targeted agent / reviewPR path the local listener uses. **This removes the
   per-worker tunnel** that the current `mago serve` needs. The worker reconnects with backoff.

License-gating: an unknown license is refused (`401`); a lapsed one (`subscription.deleted` →
plan `free`) is refused (`403`), so an unpaid worker can't (re)connect to receive events.
(`worker_links` is currently the in-memory hub; persisting it is a v2 nicety, not needed for
one worker per account.)

## The GitHub App (single ingress + repo entitlement)

The relay needs **one** public webhook ingress; a GitHub App provides it. The App is created
once by the operator and installed by each customer on their repos.

**One-time operator setup (manual — a browser step):**
1. Create a GitHub App (github.com/settings/apps/new, or a manifest flow). Webhook URL =
   `https://<platform-host>/webhooks/github/`, webhook secret = `GITHUB_WEBHOOK_SECRET`.
2. Subscribe to events: **Issues, Issue comment, Pull request** (what `classifyEvent` wakes on),
   plus **Installation** + **Installation repositories** (to keep the registry in sync).
   Permissions: Issues + Pull requests (read/write), Contents (read) — read-only is fine for v1
   since the worker still acts via its own `gh`.
3. Put the App id in `GITHUB_APP_ID` (its presence turns on repo entitlement, below).

**Per-customer (CLI, in onboarding):**
- The customer installs the App on their repos (browser, one-time). GitHub then POSTs signed
  `installation` / `installation_repositories` events; the platform records each
  `installation_id → {github_login, repos}` (the repo list is **GitHub-authoritative**).
- `mago link --installation <id>` (the id is in the install URL `.../installations/<id>`) →
  `POST /api/installations` binds that installation to the mago account.

**Repo entitlement (a real multi-tenant safety property).** Because workers self-declare
`?repos=…`, without a check worker A could subscribe to `victim/repo` and receive a *different
tenant's* relayed events. So when `GITHUB_APP_ID` is set, a worker's claimed repos are
**intersected with its account's entitled repos** (the union of repos across the installations
that account has claimed); non-entitled repos are dropped at connect and silently receive
nothing. With no App configured (local/single-tenant dev) the claimed set is trusted.

**Still operator-manual** (can't be done from code): creating the App, each customer installing
it, and pointing its webhook at the platform's public host. **v2:** GitHub OAuth at signup makes
the account↔installation binding self-verifying (today `mago link` trusts the authed claim),
and App installation tokens (RS256 App JWT) can replace the worker's `gh` PAT.

### No-App path: provision the webhook directly (operator token, zero clicks)

A GitHub App can't be created via API — creation always needs the web UI or the manifest flow
(one browser click). For operator-run / self-hosted deployments there's a fully-automatable
alternative: an **operator token** (classic PAT with `admin:repo_hook`, or `admin:org_hook` for
an org-wide hook) provisions the same ingress webhook directly. `mago-platform` does this:

```sh
mago-platform webhook add  --repo owner/repo --url https://<public-host> --account user@co.com
mago-platform webhook add  --org  acme       --url https://<public-host>   # one hook for the org
mago-platform webhook list --repo owner/repo
mago-platform webhook rm   --repo owner/repo --id <hookID>
```

It creates/updates a `web` hook for `issues, issue_comment, pull_request` pointing at
`…/webhooks/github/`, signed with `GITHUB_WEBHOOK_SECRET`. `--account` records a `repo_grants`
entitlement so that, with `GITHUB_ENFORCE_ENTITLEMENT=1`, only that account's worker receives
the repo's events (same multi-tenant safety as the App path, different source of truth). The
token is **operator-only** (read from `$GITHUB_TOKEN` or `~/.github/token`) and never reaches a
worker. Trade-off vs the App: no per-customer self-serve install UX, and the hook acts under the
operator's identity — fine for operator-run, whereas the App is the path for open self-serve.

## Onboarding (agent-driven — mago's twist on AM)

The human's own agent (e.g. Claude Code) drives the `mago` CLI; the human just approves:

```
mago register --email you@co.com        # POST /auth/signup -> token in ~/.mago/config.json
mago subscribe                          # POST /api/checkout -> prints the Stripe link; pay;
                                        #   webhook marks active + issues the license key
mago account status                     # GET /api/account -> { plan: "mago", active: true }
# install the GitHub App on your repos (browser, one-time), then:
mago link --installation <id>           # POST /api/installations -> entitles your repos
mago worker add <name>                  # registers a worker; pulls the license key
mago worker doctor                      # checks tau + gh are configured
mago company create acme                # worker's gh creates the repo, scaffolds .mago/,
                                        #   cuts mago-state, seeds the exec team
# from here: the human is CEO — files issues, answers HITL on GitHub; the exec team runs it
```

The whole loop is CLI + GitHub — no web panel. The human is the **CEO**; the seeded exec team
(CTO/CMO/Head of Product/Head of Org Engineering) runs the company.

## Build roadmap

1. **Now** — Stripe €20/mo price created ✓ + this plan.
2. **Platform backend** ✓ (`mago-platform`): accounts (email/pw, **bcrypt** cost 12, JWT) +
   **SQLite** store (pure-Go `modernc.org/sqlite`, no cgo) + Stripe checkout + webhook +
   license issuance, mirroring AM's `stripe.go` simplified to one plan.
3. **Client CLI** ✓: `register`/`login`/`subscribe`/`account status` (`account.go`) — a thin,
   secret-free HTTP client to the platform API; token + license cached in `~/.mago/config.json`.
   Verified end-to-end against real Stripe test mode. Worker license-gating is phase 4.
4. **GitHub webhook relay** ✓ (`platform/relay.go` + core `relay.go`, `mago serve --relay`):
   the platform receives repo webhooks (sig-verified) and streams them to the worker over its
   dial-out connection (NDJSON, stdlib — no WebSocket dep) — so `mago serve` no longer needs a
   public URL/tunnel. The worker authenticates with the cached `license_key`; unknown/lapsed
   subscriptions are refused. Verified end-to-end (connect, route, isolation, sig + license gates).
5. **GitHub App wiring** ✓ (`platform/relay.go` + `store.go`, core `mago link`): the platform is
   App-aware — `installation`/`installation_repositories` webhooks maintain a GitHub-authoritative
   install→repos registry, `mago link` binds an installation to an account, and worker repo
   subscriptions are **entitlement-checked** against that registry (closes a cross-tenant relay
   leak). Verified end-to-end (ping ack, registry sync incl. add/remove/delete, link claim + 404,
   entitlement drop of a spoofed repo). Remaining is operator-manual: create the App, customers
   install it, point its webhook at the public host.
6. **v2**: GitHub OAuth login (self-verifying account↔installation binding) + App installation
   tokens (replace the worker's `gh` PAT); live Stripe price; multiple workers per account.

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
- **Separate modules.** The core (root) is its own `go 1.22` module with **zero external
  deps** (stdlib-only). `platform/` is a **nested module** (`platform/go.mod`) carrying the
  billing deps (bcrypt, SQLite). The core never imports it, so removing `platform/` leaves a
  clean open-source module.
- **Build:** `go build -o mago .` (core, from root) and, because platform is its own module,
  `cd platform && go build -o ../mago-platform .` (private).
- **To open-source:** publish the root and keep `platform/` as a private overlay/submodule.
  The client commands (`register`/`login`/…) live in the core as a plain HTTP client (`account.go`,
  no secrets), so they compile without `platform/`.

Still open (not blocking): reuse AM's Stripe account for **production** vs a dedicated mago
account (test reuse is fine now). The worker↔platform transport is **decided + built**: HTTP
NDJSON dial-out streaming (stdlib, no WebSocket dep — see below).

## Data model (platform SQLite, `~/.mago-platform/platform.db`)

```sql
users (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  email           TEXT UNIQUE NOT NULL,
  password_hash   TEXT NOT NULL,                  -- bcrypt cost 12
  plan            TEXT NOT NULL DEFAULT 'free',   -- 'free' | 'mago' (the €20 plan)
  stripe_customer TEXT NOT NULL DEFAULT '',
  stripe_sub      TEXT NOT NULL DEFAULT '',
  license_key     TEXT NOT NULL DEFAULT '',       -- mago_<48hex>, issued on first payment
  created_at      INTEGER NOT NULL
)
stripe_events ( id TEXT PRIMARY KEY, seen_at INTEGER NOT NULL )  -- webhook dedup
-- worker_links (which worker relays for which repo) is the in-memory relay hub today;
-- persisting it is a v2 nicety, not needed for one worker per account.
```

No repos/runs/workers-pool tables (the client's GitHub repo is the company; the worker
journals to `mago-state`). Only what billing + relay routing need.
