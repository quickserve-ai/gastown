# Dolt Federation: Single-Syncer + Pre-Push Integrity

> **Status: Implemented (v1)** — author Woodhouse, 2026-04-23; built & deployed
> 2026-06-02 (hq-crzriu, mayor greenlight hq-wisp-mmcm9; Cherub chose keychain
> auth, hq-wisp-0de5v). The as-built shape is in "## Implementation (as built)"
> at the end of this doc; the sections below are the original design rationale.

## Problem

We've had three Dolt-remote corruption incidents in ~24 hours on `gt-hq-beads`
(v3 → v4 → v5). Same failure mode each time: GitHub chunk store ends up with a
manifest that references a `.darc` blob that was never uploaded. Any subsequent
`dolt fetch` / `dolt pull` fails with `Blob not found: <hash>.darc`.

Root cause is two-part:

1. **Concurrent pushes race on the remote manifest.** `bd` enables auto-push by
   default whenever a Dolt remote exists (`isDoltAutoPushEnabled` in
   `beads/cmd/bd/dolt_autopush.go`). Every write across every agent triggers a
   debounced push. With ~10 active agents writing every few seconds, the
   `dolt push` git+ssh transport routinely has multiple concurrent uploads.
   git+ssh has no chunk-level atomicity — one push's manifest update can land
   before another's chunks finish uploading, leaving a manifest referencing
   chunks that never arrived.

2. **Dangling references propagate through clones.** Once a remote is corrupt,
   anyone who `dolt clone`s or `dolt fetch`es that remote now has commit-graph
   references to blobs that don't exist anywhere. When they `bd dolt push` to a
   *different* fresh remote, they faithfully push the dangling reference, and
   the new remote inherits the corruption. This is why `6a9lcb82` traveled
   from v3 → local hq noms → v4 despite v4 being a "fresh" GitHub repo.

## Non-goals

- Fix upstream Dolt bugs. We don't control that project. Design around them.
- Replace git+ssh transport today. Eventually we'd migrate to S3 via
  `dolt backup sync-url`, but that's a separate workstream with AWS dependencies.
- Stop federation. Cross-town qcore and xtm federation is load-bearing for
  Alex's town collaboration — we cannot switch to "backup-only."

## Design

### 1. Single-syncer-per-town-per-database

Each shared database (`qcore`, `xtm`) has exactly one designated agent per town
that runs `bd dolt push` / `bd dolt pull`. All other agents in that town commit
to the shared local Dolt server (which every other agent in that town sees
instantly via the server, without federation).

**Our town (Gas Town)**: woodhouse is the syncer for hq + qcore + xtm.
**Alex's town**: TBD — pending Navani response.

**Cadence**: Every 5 or 10 minutes. Each town's syncer:

1. `dolt fetch origin`
2. `dolt merge origin/main` (resolving conflicts per a documented policy)
3. Run pre-push integrity check (§2)
4. `dolt push origin main` — only one push at a time per remote

Cross-town cadence should be staggered (e.g. GT at :00/:10/:20, Alex at :05/:15/:25)
so the two towns aren't racing each other on the remote manifest either.

### 2. Pre-push integrity check

Before any push, walk the local chunk graph and verify every chunk referenced
by the current HEAD's commit graph exists in the local noms store. If any
chunk is missing, abort the push with a clear error.

**Implementation sketch** (beads Go code):

- Hook point: `maybeAutoPush` in `beads/cmd/bd/dolt_autopush.go`, and the
  explicit `bd dolt push` path (likely `beads/cmd/bd/dolt_push.go` or similar).
- Use Dolt's noms API to walk the chunk graph starting from the current branch
  head.
- For each referenced chunk hash, check it exists in the local noms store
  (journal + oldgen).
- If any missing → return `ErrDanglingReference` with the chunk hash and an
  instruction to run `bd dolt verify --fix` (or similar).
- This catches the v3→v4 propagation pattern: a local DB that inherited a
  dangling reference from a corrupt clone will refuse to push it onwards.

**`bd dolt verify`** (new subcommand, stretch):

- Same chunk-graph walk, but as a standalone diagnostic.
- `--fix` mode: attempt to re-fetch missing chunks from origin, or offer to
  truncate local commit history to the last fully-intact commit.

### 3. Disable default auto-push

Already done (2026-04-23) in `~/gt/.beads/config.yaml`,
`~/gt/qcore/mayor/rig/.beads/config.yaml`, `~/gt/xtm/.beads/config.yaml`.

Longer-term: change the upstream default in `isDoltAutoPushEnabled`. "Auto-push
when remote exists" is dangerous at our concurrency level. Default should be
off; opt-in via explicit config. File upstream PR against `gastownhall/beads`.

## Implementation plan

| Phase | Work | Needs Navani? |
|-------|------|--------------|
| 1 | Auto-push disabled in GT configs | ✅ done |
| 2 | Pre-push integrity check in `bd` | ❌ no |
| 3 | `bd dolt verify` diagnostic subcommand | ❌ no |
| 4 | Formalize single-syncer policy in bridge directives | ✅ yes — cadence + Alex syncer ID |
| 5 | Upstream PR: change default auto-push to off | ❌ no (upstream review only) |
| 6 | Evaluate `dolt backup sync-url` with S3 as long-term replacement transport | ✅ yes (jointly) |

Phases 2, 3, 5 can proceed immediately. Phases 4, 6 wait on Navani.

## Open questions (for Navani)

- Does this analysis match her observations on Alex's side?
- Who is Alex town's designated syncer? (woodhouse is Gas Town's.)
- Proposed cadence — every 10 min, GT at :00/:10, Alex at :05/:15?
- Any workflow she has that depends on *fast* cross-town sync (<5 min)?
- Willing to co-sign an upstream PR changing the auto-push default?

## Rollout

1. Land pre-push integrity check in `bd` (phase 2)
2. Mayor broadcasts: "run `bd dolt push` only if you are the designated syncer"
3. Once Navani aligns: commit the syncer policy to `bridge/directives/`
4. Both syncers start running the agreed cadence; nobody else touches the remote

## What we do NOT do in this design

- Do not add application-level locking around `bd dolt push`. Coordination is
  policy + integrity check, not mutex. A mutex just hides the bug.
- Do not add retry-with-backoff on push failure. If the integrity check fails,
  the push *should* abort hard — do not paper over real corruption.
- Do not attempt to repair corrupt remotes automatically. That's a manual
  procedure (gh API to delete `refs/dolt/data`, local gc, re-push) done by the
  designated syncer when escalated.

## Implementation (as built, 2026-06-02)

Built as a macOS launchd LaunchAgent rather than a cron job, specifically to
solve the auth half of the problem (the original design left auth unspecified).

**Auth — the key insight.** The whole churn-crash (hq-crzriu) and corruption
(gt-76og) traced to one thing: ad-hoc remote ops running in contexts with **no
ssh-agent** (cron, detached agents, `bd` autopush) failed `publickey`, retried,
and churned connections until the server crashed. The fix is to run the one
syncer in a context that *has* the agent. On macOS, a LaunchAgent loaded into
the user's **Aqua (GUI) domain** (`gui/<uid>`) inherits the per-user launchd
ssh-agent (`SSH_AUTH_SOCK=/private/tmp/com.apple.launchd.*/Listeners`) that
already holds `id_ed25519` — the same agent the Dolt SQL server itself uses for
its server-side `CALL DOLT_PUSH`. `ssh-add --apple-use-keychain` reloads the key
from the login keychain so it survives reboot once the GUI session unlocks the
keychain. This is also why Barry's qcore push failed and is now moot: he just
commits locally; the syncer pushes.

**Artifacts:**
- `~/.local/bin/gt-dolt-syncer` — the cycle script (serialized, lock-guarded).
- `~/Library/LaunchAgents/com.woodhouse.gt-dolt-syncer.plist` — Aqua-domain
  agent; `StartCalendarInterval` on `:00/:10/:20/:30/:40/:50` (Gas Town's even
  tens; Alex's town staggered onto `:05/:15/...`). `RunAtLoad=false`.
- `~/.local/state/gt-dolt-syncer/` — `runs.log`, `status.json`, alert markers.

**The cycle**, per managed DB, one remote op at a time:
`gt dolt pull --db X` → integrity guard → `gt dolt sync --db X`. Both `gt dolt`
verbs route through the running server (`CALL DOLT_PULL`/`CALL DOLT_PUSH`) so the
server stays up. The script `cd`s into a Gas Town workspace first — launchd
starts at `/`, and `gt`/`bd` resolve config from CWD.

**Integrity guard (v1, deliberately lightweight).** A full §2 noms HEAD-graph
walk is the documented beads-Go follow-up. v1 is a lock-safe server-side
commit-graph walk: `select count(*) from dolt_log`. It reads every commit chunk
and errors on a dangling reference (the v3→v4→v5 propagation mode), runs in ~1s
even on the 8.6 GB hq DB, and doesn't contend with the server. A full
`dolt fsck` was rejected for the cycle — it ran **>5 min** on hq, far too heavy
to repeat every 10 min. It does NOT verify table-data chunks; that gap is the
follow-up.

**No silent stalls (mayor's requirement).** Failures are made visible by filing
a P2 bead, deduped by a per-kind marker file and self-cleared on recovery:
- auth-probe failure (the post-reboot locked-keychain stall) → bead.
- integrity-guard failure → push blocked + bead.
- push failure → per-DB streak counter; bead after 3 consecutive.

**Managed set.** Currently `xtm` + `gastown` (both verified end-to-end under
launchd: pull→integrity→push clean, 0 ahead of origin after). Held out:
- `qcore` — until gt-76og origin chunk-store repair.
- `hq` — until a one-time backlog drain (gt-loz4). On 2026-06-02 hq was 1185
  commits / 29 days behind; pushing that backlog spiked the server to ~11.2 GB
  RSS (baseline ~7.5 GB) and was SIGKILLed with zero progress (Dolt #11087).
  Retrying every cycle would OOM/jetsam the server — the exact crash this
  exists to prevent. hq rejoins `DBS` once drained to steady state, after which
  each cycle pushes only minutes of new mail/beads (cheap).

**Known limitation (accepted by Cherub).** On an *unattended* reboot the
launchd agent comes up empty and the login keychain stays locked until a GUI
login, so the syncer cannot auth. This is not silent (the auth-probe files a
bead). HTTPS + a fine-grained PAT is the documented fallback if unattended-reboot
sync ever becomes a hard requirement.
