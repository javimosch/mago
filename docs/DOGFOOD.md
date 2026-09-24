# Dogfood — mago runs mago

The cheapest proof of the north star ("agents that run companies") is to let a mago company operate
**mago's own repo**: the planner proposes improvements, the CTO ships them as PRs, the reviewer
judges them, and the CMO announces what merges. This is real activity on `javimosch/mago`,
scoped so it's safe to leave running.

## Safety posture (current)

- **Review-only:** the worker runs with `MAGO_NO_MERGE=1`, so the reviewer comments approve/changes
  but **never auto-merges** — Javi merges what's worth merging. (Flip to auto-merge by dropping the var.)
- **Label-scoped:** `MAGO_TASK_LABEL=mago` — the worker only touches `mago`-labeled issues.
- **Budget-capped:** `MAGO_DAILY_BUDGET=<n>` bounds autonomous work cycles per UTC day.
- **Scoped mission:** STATE.md `## Mission` steers the planner to low-risk, high-value work (docs,
  examples, guides, tests, DX) and explicitly says *keep the core stdlib-only; don't change locked
  architecture* (see AGENTS.md).

## Layout

Self-contained at `~/ai/mago-company` (the CLI config, the company def, and git creds all live there):

- `~/ai/mago-company/.mago/` — agents, config.json (account token + license), usage.json (budget)
- `~/ai/mago-company/STATE.md` — the **mission**
- account: `mago-dogfood@intrane.fr`, an **internal** account set `plan=mago` in the platform DB
  (non-expiring; not a paying customer), entitled to `javimosch/mago` via the claimed GitHub App
  installation (`mago link --installation <id>`).

## Run it

```sh
export HOME=~/ai/mago-company                 # isolates config from your real ~/.mago
export MAGO_PLATFORM_URL=https://mago.intrane.fr
export GH_TOKEN=$(cat ~/.github/token)        # gh + private-repo git push (or: gh auth setup-git)
export CLAUDE_CONFIG_DIR=~/.claude            # REQUIRED on Claude harness under a custom HOME (else "Not logged in")
export MAGO_TASK_LABEL=mago MAGO_GH_REPO=javimosch/mago
export MAGO_PROVIDER=claude MAGO_MODEL=sonnet  # run agents on Sonnet (local sub, no API key, no throttle)
export MAGO_PROACTIVE=3600 MAGO_PROACTIVE_MAX=1 MAGO_COMMS=1 MAGO_NO_MERGE=1 MAGO_DAILY_BUDGET=6
mago serve --relay -C ~/ai/mago-company
```

Notes:
- **Sonnet harness:** `MAGO_PROVIDER=claude MAGO_MODEL=sonnet` runs agents on your Claude Code
  subscription (no API key, no opencode-go rate limits). Because the worker uses a custom `HOME`,
  `CLAUDE_CONFIG_DIR=~/.claude` is required so claude finds its auth — otherwise every call is
  "Not logged in". (To use tau instead, set `OPENCODE_API_KEY` and drop the two MAGO_PROVIDER lines.)
- **Mission persistence:** set the mission in `~/ai/mago-company/STATE.md` `## Mission`. The worker
  preserves a locally-set mission when it syncs the `mago-state` branch (it no longer gets clobbered
  by a stale remote STATE).
- `MAGO_PROACTIVE=3600` = propose hourly (smaller to test); `MAGO_PROACTIVE_MAX=1` = one proposal
  per cycle. Check in with `HOME=~/ai/mago-company mago digest -C ~/ai/mago-company`.

## The loop

planner reads the mission → files `mago`-labeled issues → CTO clones + implements → opens a PR →
reviewer comments (no merge) → you merge → CMO posts a release note on merge. All under the daily cap.

## Make it persistent

For 24/7, run the above on **dk1** (or any always-on host) as a daemon (systemd/`@reboot`/superbg),
with `OPENCODE_API_KEY` in the environment. It dials out over the relay; no inbound port. Keep
`MAGO_NO_MERGE=1` until you trust it enough to drop it.
