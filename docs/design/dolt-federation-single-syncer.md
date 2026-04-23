# Dolt Federation: Single-Syncer + Pre-Push Integrity

> **Status: Proposed** — author Woodhouse, 2026-04-23. Waiting on Navani response
> (xt-sbj) to finalize cross-town cadence. Implementation can start on the
> integrity-check half without her input.

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
