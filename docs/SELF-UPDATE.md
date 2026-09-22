# Self-update

mago implements [cli-update-spec](https://github.com/javimosch/cli-update-spec) v1.1 for the
`mago` binary: a content-hash version (`sha256[:12]` of the artifact), a `GET /version`
endpoint on the platform, an `update` command, a passive nudge, and `install`/`uninstall`.

The version is a hash, not a semver — identical bytes never trigger an update, and converging
on the published artifact is the intent even when that means moving *back* to it (the server
is authoritative; there is no newer/older, only same/different). Publish the artifact before
or with the deploy, never after.

## `mago update`

```
mago update [--check] [--force]
```

Flow (spec §3): compute the running binary's hash → `GET /version?os=&arch=` → compare →
download → verify `sha256[:12]` (and the full `sha256` when advertised) → smoke-test
(`mago version` must run) → atomic swap: current binary moves to `<exe>.bak`, new one in.

- The `.bak` is **kept** on success — roll back by hand with `mv mago.bak mago`.
- If the swap fails mid-way, the `.bak` is restored automatically; mago is never left
  without a runnable binary.
- A `<exe>.lock` flock serializes concurrent updates, so two workers sharing one binary
  path can't interleave their `.bak` moves.
- stdout carries a JSON result; progress goes to stderr.

| Exit | Meaning |
|---|---|
| 0 | updated, or already up-to-date |
| 5 | `--check` only: an update is available (not an error) |
| 80 | bad flags |
| 90 | the binary's directory isn't writable by this user |
| 100 | fetch/download/verify/smoke/swap failure |

`--force` re-downloads and swaps even when the version matches — the repair path for a
corrupt binary.

## `mago install` / `mago uninstall` (spec §6)

```
mago install [--prefix DIR]      # default ~/.local/bin — no sudo
mago uninstall [--prefix DIR]
```

`install` **copies** the running binary to `<prefix>/mago` and marks it executable —
idempotent, never touches rc files or `$PATH`, and fails with exit `90` rather than
escalating when the prefix isn't writable. `uninstall` removes just that file; a missing
file is a no-op success.

**Install where the worker's user owns the directory.** The self-update swap stages a temp
file beside the binary, so the directory must be writable by the user mago runs as. For a
fleet running under a no-root `agent` user that means the agent's own `~/.local/bin` — *not*
a root-owned shared prefix like `/usr/local/bin`. If a worker is already installed somewhere
unwritable, relocate it as the worker user:

```
mago install                       # copies the current binary to ~/.local/bin/mago
~/.local/bin/mago update           # pulls the latest release into the new location
# then re-point the launcher's PATH/binary at ~/.local/bin/mago and restart the worker
```

## Worker modes

- `update=manual` (default): on each relay ping the worker compares the advertised version
  and prints a **one-time nudge** to stderr — spec §4. The nudge never updates anything.
- `update=auto` (`mago mode update=auto` / `mago worker mode update=auto --all` /
  `MAGO_UPDATE=auto`): mago's opt-in extension beyond the spec — the worker downloads,
  verifies, smoke-tests, swaps (with `.bak`), and re-execs itself on the advertised version.

If `update=auto` hits a permission failure (binary's directory not writable by the worker
user), the error is reported **once** with the path and user, then the worker falls back to
the passive nudge for that process — it does not retry an operation that cannot succeed
until the filesystem changes (this was the 163-failures-per-day loop on rbm4).

## Platform side

`GET /version?os=&arch=` (open, no auth) returns `{"ok":true,"version","download","sha256"}`
computed from the actual artifact on disk — `mago-<os>-<arch>` in `MAGO_CLI_DIR`, or the
legacy `MAGO_CLI_BINARY` fallback for linux/amd64. `404 {"ok":false,"error"}` when nothing
is published. The same version is advertised on relay `ready`/`ping` frames.
