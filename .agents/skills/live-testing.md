# Live testing

How to exercise mago for real. Prereqs: `tau` + `gh` on PATH and authenticated; for agent ticks
use `MAGO_PROVIDER=opencode-go MAGO_MODEL=deepseek-v4-flash` (the personas' `deepseek` provider
needs an API key and otherwise fails with tau code 110).

> **Configure your provider key or you'll get throttled.** mago's `runTau` passes no `--api-key`,
> so tau resolves the key itself (after tau#30, precedence is: `--api-key` → config
> `keys[provider]` → provider env var → config `api_key` → `TAU_API_KEY` → keyless/builtin). If
> none is set, tau hits a **rate-limited keyless/builtin path**; under a batch run it exhausts and
> heavy multi-iteration ticks fail with `tau code 110` (cheap 1-shot calls still slip through, so
> it looks intermittent — a `110` that only hits big ticks means "no key for this provider").
> **Recommended:** put your subscription key in `~/.config/tau/config.json` (chmod 600):
> `{"keys": {"opencode-go": "sk-..."}}` — then it's used automatically, no env needed. Or export
> `OPENCODE_API_KEY` (`DEEPSEEK_API_KEY`/`OPENAI_API_KEY` for those). `mago serve` warns at startup
> if neither env nor config provides a key for the resolved provider.

> Network + long-running/background processes: the Bash sandbox blocks outbound network and
> process control (you'll see exit 144). Run live steps with the sandbox disabled. Long-lived
> processes (the platform, the worker) must BE the background command (they don't exit), not a
> wrapper script that backgrounds them (the wrapper's exit reaps the child).

## Build

```sh
go build -o mago .                                   # core
cd platform && go build -o ../mago-platform . && cd ..
```

## Local platform smoke

```sh
cp platform/.env.example platform/.env   # fill secrets (real sk_test_ reusable from AM's .env)
./mago-platform start --port 9100        # or: start --daemon
curl -s localhost:9100/healthz           # ok
# signup → account(free) → checkout(real Stripe test URL); a SIGNED stripe webhook activates:
#   t=$(date +%s); body='{"id":"evt_x","type":"checkout.session.completed","data":{"object":{"metadata":{"user_id":"1","plan":"mago"}}}}'
#   sig=$(printf '%s.%s' "$t" "$body" | openssl dgst -sha256 -hmac "$STRIPE_WEBHOOK_SECRET" | sed 's/.* //')
#   curl -X POST localhost:9100/stripe/webhook -H "Stripe-Signature: t=$t,v1=$sig" --data-raw "$body"
```

## Operator simulation (does the CLI drive itself?)

Give an LLM agent (e.g. `opencode run -m opencode-go/deepseek-v4-flash "<goal>"`) only the `mago`
CLI + a goal, and confirm it can register/verify/scaffold/connect from `mago help` alone. CLI
friction it hits = bugs to fix. (This is how the `project add owner/repo` shorthand + `project
list` were found.)

## The capstone: operator files an issue → agents ship a PR (live, through the platform)

1. Have an **active** account (real test-card checkout, or a signed webhook) and its license in
   `~/.mago/config.json`; the account must be **entitled** to the target repo (App install +
   `mago link`, or a `repo_grant`).
2. Start the worker against the repo, persistent, on the live relay:
   ```sh
   MAGO_GH_REPO=javimosch/<repo> MAGO_PROVIDER=opencode-go MAGO_MODEL=deepseek-v4-flash \
     MAGO_PLATFORM_URL=https://mago.intrane.fr ./mago serve --relay -C <company>
   ```
   Look for `[relay] connected to https://mago.intrane.fr for repos [...]`.
3. As the operator, file a small self-contained task (creates a GitHub issue → relay wakes the
   worker): `MAGO_GH_REPO=javimosch/<repo> ./mago task add "Add a CONTRIBUTING.md ..." --project <name> -C <company>`.
4. Watch the loop: `[relay] issues -> issue #N opened` → route → implementer opens a PR →
   `[relay] pull_request -> PR #M opened` → `reviewPR` judges + merges → issue closed, file on
   the default branch. Verify with `gh pr view` / `gh issue view`.

Proven end-to-end on 2026-06-22 (issue → PR merged through mago.intrane.fr). Pick a tiny,
self-contained task (a doc/file) so a run stays quick and the artifacts are easy to review, and
clean up test issues/PRs after.
