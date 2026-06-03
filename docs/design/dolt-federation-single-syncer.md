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

**Managed set.** `DBS=(gastown qcore xtm)` as of 2026-06-03. Timeline:
`gastown` re-enabled 2026-06-02 (post-2.1.1, mayor gate-b SIGNED; manual
kickstart + natural `:40` tick both verified clean). `qcore` rejoined 2026-06-02
(gt-76og origin repair + union-replay). `xtm` rejoined 2026-06-03 once vector
#11's bridge pull-only fix shipped (PR #10) — pushed only on even-ten marks per
the cross-town stagger (see the stagger note in the re-enable section). Still
held out:
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

## The Single-Remote-Op Invariant (2026-06-02, hq-crzriu re-enable gate)

> **Status: design — mayor gate (b).** Written by Woodhouse during the 2.1.1
> crash-watch. The mayor holds syncer + 3-plugin re-enable until BOTH (a)
> crash-stability confirmed on 2.1.1 past ~16:30 AND (b) this invariant blessed.
> This section IS gate (b). The point of single-syncer is worth nothing if a
> *second* actor still touches a shared Dolt remote concurrently — that second
> actor is exactly the manifest-race that corrupts the chunk store (§Problem).

### Invariant (one sentence)

> **At any instant, for any shared Dolt remote, at most one process is
> performing a chunk-store remote op (`fetch`/`pull`/`push`), and that process
> is the designated single-syncer LaunchAgent.**

Local commits to the shared 3307 server are unrestricted — every agent sees
them instantly via the server, *without* federation. The invariant constrains
only ops that talk to a GitHub remote (the corruptible manifest).

### Every remote-op vector, enumerated, with closure

The whole job of gate (b) is to prove the enumeration is *complete* — a vector
we forget is a vector that re-corrupts the remote. Audited 2026-06-02:

| # | Vector | Mechanism | Touches Dolt remote? | Closure (current) |
|---|--------|-----------|----------------------|-------------------|
| 1 | **Single-syncer LaunchAgent** | `gt dolt pull` → integrity → `gt dolt sync` (server-side `CALL DOLT_PULL/PUSH`), serialized, lock-guarded | **YES — the one allowed** | PAUSED (`launchctl bootout`); the sole sanctioned op when re-enabled |
| 2 | **`bd` autopush** (inline + gt-8cy3 detached worker) | `maybeAutoPush` forks `bd dolt _autopush-worker`; fires on every write when a Dolt remote exists | YES | `dolt.auto-push: false` in **all** shared configs. hq/qcore/xtm had it; **gastown was open in 4 of 5 contexts** (woodhouse, refinery/rig, barry-s9w7z, barry-omp-fix) — **closed 2026-06-02 this session**; mayor/rig already had it |
| 3 | **crosstown-sync plugin** | `run.sh` → `gt dolt pull --db X` (server-side fetch) on a 10m cooldown gate | YES | gate=`manual` across town + all dog/rig copies; runtime symlink dropped (bridge source intact) |
| 4 | **dolt-archive plugin** | remote op on cooldown gate | YES | gate=`manual` across all copies |
| 5 | **dolt-backup plugin** | remote op on cooldown gate | YES | gate=`manual` across all copies |
| 6 | **daemon `dolt_backup` patrol** | `mayor/daemon.json` → 2h interval | YES (to *backup* remote) | **enabled in daemon.json** but inert via rig-level `backup.enabled: false` (gt-yuhb). ⚠ Gate is at the wrong layer — see Fragile Gates below |
| 7 | **daemon `jsonl_git_backup` patrol** | 15m, pushes JSONL export to the *project git* repo | NO — git repo, not the Dolt chunk store; cannot race the Dolt manifest | left enabled; out of scope for this invariant |
| 8 | **manual `gt dolt sync` / `bd dolt push`** by any agent | human/agent runs it directly | YES | policy only — "only the designated syncer pushes." Documented in CLAUDE.md Dolt core; not machine-enforced |
| 9 | **moshi-hooks** (SessionStart/Stop/PreToolUse/PostToolUse/SubagentStop) | `bunx moshi-hooks` on every lifecycle event | **NO** | moshi-hooks 1.1.1 is a telemetry/observability dispatcher (`setup`/`token` only — phones home to its API). It does **not** run `xtm sync`, `dolt pull`, or any Dolt remote op. The mayor flagged this as the "session-start sync vector"; audit shows it is **benign for Dolt** — recorded here so we don't re-litigate |
| 10 | **Stop hook `bd sync`** | `~/.claude/settings.json` Stop hook | NO | `bd sync` is **not a command** (`unknown command "sync"`); the hook errors and is swallowed by `|| true`. Inert |
| 11 | **`xtm inbox` / `xtm send` auto-push** | `bridge/bin/xtm`: `inbox` runs `xtm sync` "pull before reading"; `send` runs it twice (pull before, push after). `xtm sync` does pull **AND push** (`gt dolt sync --db xtm` → `CALL DOLT_PUSH` + `git push --force refs/dolt/...`) | **YES — on xtm, a syncer-MANAGED remote** | **OPEN (found by mayor via ps, 2026-06-02 14:56/14:57).** Not a hook — a side-effect of the wrapper CLAUDE.md tells *every agent in both towns* to use. Every cross-town mail check/send pushes the xtm remote. Proposed closure below; needs overseer approval (bridge file, affects Alex's town). **CLOSED 2026-06-03 — bridge PR #10 (`fix(xtm): keep routine reads and sends pull-only`, squash 541f403) made `inbox`/`send` pull-only; `xtm sync` kept as the syncer's pull+push path.** |

**Conclusion of the audit:** after the mayor's plugin/syncer mitigations, the
#2 gastown-autopush closure, and the #11 bridge pull-only fix (PR #10, shipped
2026-06-03), **all enumerated vectors are closed.** The (historical) analysis of
#11 follows. The
`xtm inbox`/`xtm send` commands auto-invoke `xtm sync`, whose *push* half
(`gt dolt sync --db xtm`) writes the xtm shared remote. Because every agent in
*both* towns runs `xtm inbox`/`send` routinely (CLAUDE.md mandates it), the xtm
remote has had many concurrent pushers all along — almost certainly a dominant
xtm-corruption driver, and a direct collision with the syncer's xtm push on
re-enable. The session-start vector the mayor originally worried about (#9,
moshi-hooks) is benign; the *real* session-activity vector is #11.

### Vector #11 — `xtm`-wrapper auto-push (SHIPPED 2026-06-03)

**As-built:** bridge PR #10 (squash `541f403`, branch `alex/xtm-pull-only`,
authored by Alex's town, co-signed + overseer-approved) implemented the proposed
closure verbatim: added `xtm pull` / `xtm push` paths; `xtm inbox` and `xtm send`
now call `xtm pull` (pull + notify, **no push**); `xtm sync` keeps pull+push as
the single-syncer command; sync/push failures now propagate non-zero for syncer
supervision; added `test-xtm.sh` (6 regression tests, all green). `xtm` rejoined
the syncer `DBS` the same day (see managed-set note below). The original analysis
that motivated the fix is preserved below.

**Why push, not pull, is the problem.** A `dolt fetch`/`pull` is read-only on
the remote manifest; concurrent pulls don't corrupt it. The incident (§Problem)
is concurrent **pushes** racing the manifest write. So `xtm inbox`'s
*pull-before-read* is acceptable; its (and `send`'s) *push* is not.

**Proposed fix (bridge change — overseer + Alex's town must co-sign):** decouple
pull from push in the auto-sync paths. `xtm inbox`/`send` should **pull + notify
only**, never push. Concretely, add an `xtm _pull` path (`gt dolt pull --db xtm`
+ `xtm-notify`) and call *that* from `inbox`/`send` instead of full `xtm sync`.
Rationale: a sent message is a local commit on the shared 3307 xtm DB the moment
`bd create` runs; the single-syncer pushes it to the remote on the next `:X0`
mark (≤10 min). That trades sub-10-min cross-town delivery latency for
manifest-safety — acceptable, and exactly the single-syncer bargain. `xtm sync`
(explicit) keeps pull+push for the designated syncer.

**Both towns.** Alex's town runs the same `bridge/bin/xtm`, so *their* agents
also push the xtm remote on every inbox/send. The fix must land in the bridge
for both towns, or one town keeps corrupting the shared remote. This is why it
goes through the overseer, not a unilateral edit.

### Fragile gates (hardening follow-ups, not re-enable blockers)

- **#6 daemon `dolt_backup` is gated at the wrong layer.** It is `enabled: true`
  in `mayor/daemon.json` and stays inert *only* because every rig config carries
  `backup.enabled: false`. A single rig clone created without that line silently
  re-arms a 2h remote op. Correct fix: set `dolt_backup.enabled: false` in
  `mayor/daemon.json` so the gate lives at the daemon, not scattered across N
  rig configs. (Follow-up bead — safe to do now; does not re-enable anything.)
- **#2 autopush default is upstream-wrong.** `isDoltAutoPushEnabled` defaults ON
  whenever a remote exists. Every new clone re-opens vector #2 until someone
  remembers the flag. The durable fix is the upstream PR (Phase 5) flipping the
  default to off; until then, single-syncer correctness depends on a config line
  no one forgets. Track per-clone.
- **#8 + #3 are policy/gate, not locks.** By design (§"What we do NOT do" —
  no app-level mutex). Acceptable because the syncer serializes its own ops and
  the integrity guard refuses to push a dangling reference; a stray manual push
  is caught by the same guard rather than a lock.

### Re-enable checklist (the mayor's gate, made concrete)

Re-enable ONLY when **both** hold:

1. **Crash-stability (gate a):** PID 30117 (Dolt 2.1.1) survives past ~16:30
   with no silent death — clears the ~hourly 12:45/14:06/15:05 cadence.
2. **Invariant blessed (gate b):** this section, plus the audit above showing
   every vector closed (esp. #2 gastown, now done).

Then, in order:
1. `gt plugin sync` — restores crosstown-sync / dolt-archive / dolt-backup
   from gate=`manual`. **Before flipping their gates back**, confirm their
   cooldown-driven remote ops won't run concurrently with the LaunchAgent's
   `:X0` marks — stagger or leave the pull-plugins gated if the syncer already
   covers their DBs (the syncer's `gt dolt pull` makes crosstown-sync redundant
   for managed DBs).
2. Re-bootstrap the LaunchAgent: `launchctl bootstrap gui/$(id -u)
   ~/Library/LaunchAgents/com.woodhouse.gt-dolt-syncer.plist` (RunAtLoad=false,
   so it waits for the next `:X0`).
3. Watch `~/.local/state/gt-dolt-syncer/runs.log` + `status.json` for one clean
   cycle per managed DB.

**Managed set (as-built, 2026-06-03).** `DBS=(gastown qcore xtm)`. History:
`gastown` re-enabled first (no everyday-agentic-push path; #2 autopush disabled);
`qcore` rejoined 2026-06-02 (gt-76og origin repair + union-replay); `xtm` rejoined
2026-06-03 the moment #11's pull-only fix shipped (PR #10). `hq` remains held
pending its one-time backlog drain (gt-loz4).

**xtm cross-town push stagger.** Because `xtm` is shared with Alex's town, the
syncer must not push it concurrently with Alex's syncer. Co-signed convention:
**our town pushes xtm only on EVEN ten-minute marks (:00/:20/:40); Alex's town
pushes on ODD marks (:10/:30/:50)** — one bounded pusher per town, never
overlapping. Enforced by `gt-dolt-syncer`'s stagger guard (`STAGGERED_DBS=(xtm)`
+ `is_our_push_window`): on an odd-ten cycle the syncer still pulls xtm
(read-only, safe every cycle) but **defers the push**, logging `odd-ten mark —
push deferred to Alex's town`. `gastown`/`qcore` are single-town and push every
cycle. Alex's xtm cron stays OFF until a coordinated 'go' confirms our bounded
pusher is live (verified clean cycle 2026-06-03: xtm pull+integrity OK, odd-ten
deferral correct, controlled even-window push `1 pushed 0 failed`).
