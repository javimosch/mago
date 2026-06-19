# AGENTS.md — coding guidelines

Guidelines for working in the mago codebase. Inherited from the
`boilerplate-cli-ui-go-v2-react` scaffold and adapted for mago. Read
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) first for the design.

## Agent-first by default

mago's CLI is driven by agents (the client's own agent does onboarding; mago's worker
agents call `mago` and `gh` via bash). Design every command for a machine caller first:

- **stdout** = JSON data / command results only.
- **stderr** = logs, progress, human-readable errors.
- Provide `--json` on anything a script or agent would parse.
- Make commands idempotent and scriptable; no interactive prompts on the happy path.

## File size limits

- **Go:** max **500 LOC** per file. Split when you exceed it — extract, don't append.
- **React component/view:** max **300 LOC**.
- **CSS:** max **300 LOC** (prefer Tailwind utilities).

## Go file organization

Keep responsibilities separated by file, e.g.:

| File | Responsibility |
|---|---|
| `main.go` | command routing, flag parsing, help text |
| `server.go` | HTTP handlers, embedded FS, JSON responses |
| `daemon.go` | PID file, signals, daemon lifecycle |

mago-specific areas (keep each small and single-purpose): account/auth, Stripe, worker
runtime, reconcile loop, tau driver, gh adapter, git/worktree, journal/state I/O.

## Build split (platform vs client)

Binaries are split (see ARCHITECTURE). Keep operator-only code (platform daemon, Stripe,
webhook relay) out of the client binary — use separate packages and Go build tags so the
client build cannot include `daemon start/stop` or any platform control surface.

## Semantic exit codes

```
0       success
80–89   user errors (invalid input, permission denied)
90–99   resource errors (not found, already exists)
100–109 integration errors (network, timeout, GitHub/Stripe/tau failures)
110–119 software errors (internal, unexpected)
```

Scripts and agents branch on `$?`; keep the mapping stable.

## Keys & secrets

- The client's **LLM provider key never leaves the worker** and must never be sent to the
  platform backend (BYOK invariant).
- Local config at `~/.mago/config.json`, permissions `0600`.
- Never log tokens or keys; redact in error output.

## External tools

The worker depends on **tau** and **gh** being installed and configured. Detect and
auto-configure via `worker doctor`; fail with a clear `100–109` integration error (not a
crash) when they're missing or unauthenticated.

## git-native state

- Agent definitions live on `main`; all runtime exhaust on the orphan `mago-state` branch.
- Journals are **append-only** — write new timestamped files, never edit existing ones.
- An agent writes only under its own `runs/`, `state/`, `memory/` paths.
- The worker is the sole writer to `main`.
- Skills (`.mago/skills/`) and decisions (`.mago/decisions/`) live on `main` as shared,
  improvable knowledge; the worker serializes writes so they stay conflict-free.

## Embedded UI pitfalls (if/when a UI is added)

- `//go:embed` paths are relative to `go.mod`; embed `ui/*` with `ui/` at the repo root.
- Don't embed test files.
- Use `fs.Sub` to serve a subdirectory.

## Docs discipline

- Every feature ships a short doc under `docs/`.
- Keep `docs/ARCHITECTURE.md` authoritative; update it when a locked decision changes.
- Use kebab-case for doc filenames.
