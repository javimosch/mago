# Deploying mago-platform

The operator-private control plane runs at **https://mago.intrane.fr** on the **dk1** VM
(`vpspoly1`), behind Traefik (TLS via Let's Encrypt) → `localhost:9100`.

```
GitHub / Stripe / workers ──HTTPS──> Traefik (dk1) ──> 127.0.0.1:9100 mago-platform
                                         │ mago.intrane.fr, Let's Encrypt http-01
                                         └ DNS A record via Cloudflare (intrane.fr zone)
```

## Build (static linux/amd64)

`modernc.org/sqlite` is pure Go, so a fully static binary works (no glibc dependency):

```sh
cd platform && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../mago-platform .
```

## Process lifecycle (the `start`/`stop`/`status` daemon)

`mago-platform` is its own daemon (see `platform/daemon.go`):

```sh
mago-platform start --port=9100      # foreground (what hotify's cmd runs / supervises)
mago-platform start --daemon --port=9100   # detach: pidfile + logfile in ~/.mago-platform/
mago-platform status                 # running (pid N) | stopped
mago-platform stop                   # SIGTERM the pid
```

It loads `.env` from (priority) `$MAGO_PLATFORM_ENV`, `platform/.env` (dev), then **the
directory of the binary** — so a deploy just drops `.env` next to the binary.

## What's deployed on dk1

```
/home/dk1/mago-platform/mago-platform   # the static binary
/home/dk1/mago-platform/.env            # prod secrets (NEVER in git)
/home/dk1/mago-platform/platform.db     # SQLite (DB_PATH)
~/.mago-platform/mago-platform.{pid,log}
crontab: @reboot /home/dk1/mago-platform/mago-platform start --daemon --port=9100
```

`.env` keys in prod: `APP_URL=https://mago.intrane.fr`, `PORT=9100`, `JWT_SECRET`,
`STRIPE_SECRET_KEY` (sk_test), `STRIPE_WEBHOOK_SECRET` (from the Stripe endpoint below),
`STRIPE_PRICE_MAGO`, `GITHUB_WEBHOOK_SECRET`, `GITHUB_ENFORCE_ENTITLEMENT=1`,
`DB_PATH=/home/dk1/mago-platform/platform.db`.

Stripe webhook endpoint (test mode): `we_1TkslJ…` → `https://mago.intrane.fr/stripe/webhook`
(`checkout.session.completed`, `customer.subscription.deleted`); its signing secret is the
`STRIPE_WEBHOOK_SECRET` in `.env`.

## Deploy / redeploy

```sh
cd platform && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../mago-platform .
scp mago-platform dk1:/home/dk1/mago-platform/mago-platform
ssh dk1 'cd ~/mago-platform && ./mago-platform stop; ./mago-platform start --daemon --port=9100 && curl -fsS localhost:9100/healthz'
```

## hotify (DNS + Traefik)

hotify-cli manages the Cloudflare DNS record and Traefik route:

```sh
hotify-cli setup --id mago --name 'mago platform' --domain mago --port 9100 \
  --cmd '/home/dk1/mago-platform/mago-platform start --port=9100'
hotify-cli setup-dns     --id mago --ip 92.113.145.178
hotify-cli setup-traefik --id mago --challenge-type http   # run AFTER DNS resolves
```

**Gotchas hit during the first deploy** (so the next one is smooth):

- **hotify auth.** The default remote token (`admin-bootstrap`) expired 2026-06-21 → every
  remote call `401`. Fixed by authing with the `new-admin` key (valid to 2026-06-27) and making
  it the default remote in `~/.hotify/config.json`. **Renew these keys** (`hotify-cli api-keys`)
  before they lapse.
- **`setup` is local-only.** It writes the laptop's `~/.hotify/config.json` but does **not**
  register the app on the remote daemon, and `deploy` (which would) returns `404`/`401` in this
  hotify version. So the binary is shipped with `scp` and the app row was added to the remote
  `/home/dk1/.hotify/config.json` by hand (clone of `automaintainer-panel`). `setup-dns` /
  `setup-traefik` then work because the daemon can see the app.
- **Order: DNS before Traefik.** http-01 ACME validation needs `mago.intrane.fr` to resolve, or
  `setup-traefik` times out.

## Verify

```sh
curl -fsS https://mago.intrane.fr/healthz                 # ok
curl -fsS -X POST https://mago.intrane.fr/auth/signup -d '{"email":"a@b.c","password":"hunter2pass"}'
# checkout returns a real Stripe session URL; /ws/worker and /webhooks/github reject (401) without creds
```

## Not yet (follow-ups)

- A **systemd unit** would be sturdier than the `@reboot` crontab (needs root on dk1).
- **Live Stripe** (`sk_live_`) + a live price for production billing.

## GitHub App (live, App-mode)

The platform runs in **App-mode**: GitHub App **`mago-platform`** (id `4111043`, owner
`javimosch`, created via `mago-platform setup-github`) delivers webhooks. dk1's `.env` carries
`GITHUB_APP_ID`, the App's `GITHUB_WEBHOOK_SECRET`, and `GITHUB_APP_PRIVATE_KEY=/home/dk1/
mago-platform/github-app.pem`; entitlement is enforced from App installations.

- Install on repos: `https://github.com/apps/mago-platform/installations/new`
- On install, GitHub posts a signed `installation` event → the platform records
  `installation_id → repos`; bind it to an account with `mago link --installation <id>`.
- Verified end-to-end (2026-06-22): install (all repos) → `mago link` → a real `mago-poc` issue
  flowed App → mago.intrane.fr (App-secret verified) → relay → worker, entitlement from the
  installation alone.
- App private key + creds: `~/.mago-platform/github-app.{pem,env}` on the operator machine.
  The pre-App operator-token hook path was removed; old secret backups in `dk1:~/mago-platform/
  .env.pre-app-bak`.
