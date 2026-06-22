# Metrics — answering "stats on mago prod/live/dk1 activity"

When the operator asks for **stats / metrics / activity** on the live mago deployment, this is
where the numbers come from. Everything below is **read-only** — never mutate prod or print secrets.

## Where prod lives
- Host: SSH `dk1` (the `vpspoly1` VM). Platform dir: `/home/dk1/mago-platform/`.
- Binary `mago-platform`, SQLite DB `platform.db` (path shown in the startup log: `store=…`).
- Public base: `https://mago.intrane.fr` (Traefik TLS → `localhost:9100`). Full runbook: `docs/DEPLOY.md`.

## First stop: the activity command
The fastest answer — account breakdown + a newest-first event timeline:
```sh
ssh dk1 'cd ~/mago-platform && ./mago-platform activity 30'   # last 30 events; omit N for 20
```
Output: `accounts: N total — A active, T trial-live, E trial-expired, F free` then a timeline of
`signup / subscribed / canceled / worker_connect / worker_disconnect / linked` events (each with
timestamp + account email). This answers "who signed up?" and "did their worker come online?".

## Live health
```sh
curl -fsS https://mago.intrane.fr/healthz            # "ok" if up
ssh dk1 'cd ~/mago-platform && ./mago-platform status'   # running (pid) | stopped
ssh dk1 'tail -50 ~/.mago-platform/mago-platform.log'    # request/relay/stripe log lines
```

## Ad-hoc numbers (SQLite, read-only)
The `events` + `users` tables back everything; query directly for anything the command doesn't show.
WAL allows reading while the daemon runs. Examples:
```sh
ssh dk1 'sqlite3 ~/mago-platform/platform.db "
  SELECT plan, COUNT(*) FROM users GROUP BY plan;                                  -- plan mix
  SELECT COUNT(*) FROM events WHERE kind=''signup''  AND ts > strftime(''%s'',''now'',''-7 days'');  -- signups last 7d
  SELECT COUNT(DISTINCT user_id) FROM events WHERE kind=''worker_connect'';        -- accounts that ever connected a worker
  SELECT date(ts,''unixepoch''), kind, COUNT(*) FROM events GROUP BY 1,2 ORDER BY 1 DESC;"'
```
Tables: `users(plan,trial_ends,stripe_*,created_at)`, `events(ts,kind,user_id,detail)`,
`installations`, `repo_grants`. Funnel = `signup → worker_connect → subscribed`. Active MRR ≈
`active × €20`. Trials live vs expired comes from `plan='trial'` + `trial_ends` vs now.

## Agent throughput (GitHub side)
"How much code did mago ship?" lives on the **customer repos**, not the platform. Find entitled
repos in the DB (`installations.repos_json`, `repo_grants`), then per repo:
```sh
gh pr list  -R owner/repo --state merged --search "head:mago/" --limit 100 --json number,mergedAt
gh issue list -R owner/repo --state closed --label mago --limit 100
```
Count merged `mago/task-*` PRs (deliverables shipped) and closed `mago`-labeled issues (tasks done).

## Push notifications
Real-time push (Telegram) is **postponed** — see issue javimosch/mago#10. Until then, metrics are
pull-based via the command + queries above.
