# Dolt hq Remote Rebuild Runbook

**Bead:** gt-3y4
**Status:** drafted, awaiting Cherub authorization
**Risk:** medium — destructive operations on remote git state
**Blast radius:** hq Dolt remote only (other 5 remotes unaffected)
**ETA:** ~15 min execution + 30 min observation

## Background

The hq Dolt database (which holds town beads, mail wisps, and operational
state) syncs to a single git+ssh remote at
`git+ssh://git@github.com/quickserve-ai/gt-hq-beads-v5`.

Symptom: every fetch attempt to `origin` fails with:

```
failed to get remote db; the remote: origin '...' could not be accessed;
Blob not found: <hash>.darc
```

The remote claims it does not have a `.darc` archive index file that local
Dolt's manifest references. Until the local manifest and remote storage are
brought back into sync, hq cannot push to GitHub. Local writes still succeed;
this is a sync failure, not a data-loss failure.

Yesterday's surgical fix (Cherub-authorized, ~17:00 PDT 2026-05-03) used
`gh api -X DELETE refs/dolt/data` + `dolt push --force` and resolved the
prior corrupt blob. A *new* blob (`90e15fd6papm265h5pvj1l7v0368ckst.darc`)
appeared during normal operation today (first error 15:02:31), suggesting
the production write path is producing manifest entries the push path
cannot reliably upload.

**Order of operations:** root-cause investigation FIRST, then rebuild.
Re-running the surgical recipe without finding the cause buys ~24h of
quiet, then we are back here. See Phase 1 below.

## Phase 0 — Pre-flight forensics (read-only, run anytime)

Goal: preserve evidence before fixing, so we can diagnose root cause after.

```bash
SNAP="/tmp/dolt-rebuild-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$SNAP"

# Server + log state
gt dolt status > "$SNAP/dolt-status.txt" 2>&1
gt dolt logs -n 20000 > "$SNAP/dolt-logs.txt" 2>&1

# Local hq state
( cd ~/gt/.dolt-data/hq
  dolt log -n 20 --oneline > "$SNAP/hq-local-log.txt"
  dolt log -n 1 main       > "$SNAP/hq-local-head.txt"
  dolt remote -v           > "$SNAP/hq-remotes.txt"
  ls -la .dolt/noms/       > "$SNAP/hq-noms.txt"
  cat .dolt/manifest       > "$SNAP/hq-manifest.txt" 2>/dev/null
)

# Remote ref state (what GitHub thinks HEAD is)
gh api repos/quickserve-ai/gt-hq-beads-v5/git/refs/dolt/data \
  > "$SNAP/remote-refs-dolt-data.json" 2>&1
gh api repos/quickserve-ai/gt-hq-beads-v5/branches/main \
  > "$SNAP/remote-main.json" 2>&1

# Distinct blob hashes failing in current logs
grep "Blob not found" "$SNAP/dolt-logs.txt" | \
  awk -F'Blob not found: ' '{print $2}' | awk -F'"' '{print $1}' | \
  sort -u > "$SNAP/missing-blobs.txt"
```

A wrapper script lives at `scripts/dolt-hq-rebuild-forensics.sh` — run that
to capture everything in one shot.

**Stop and inspect** the forensics. Confirm:
- Only 1 distinct missing blob? (currently `90e15fd6papm265h5pvj1l7v0368ckst.darc`)
- Local HEAD recorded for rollback comparison
- Remote `refs/dolt/data` SHA recorded for rollback comparison

## Phase 1 — Root cause investigation (read-only)

Do this BEFORE Phase 2+. The point of investigation is to decide whether
Option A (cleanup) suffices, whether Option B (full rebuild) is required,
and — most importantly — whether there is an upstream write-path bug that
will recreate the corruption regardless of which option we run.

### Test 1: Is the local manifest internally consistent?

```bash
( cd ~/gt/.dolt-data/hq
  dolt admin storage 2>&1 | head -30 || echo "dolt admin not available"
  # If dolt admin reports orphaned/dangling blobs in noms/, manifest is corrupt
)
```

### Test 2: Does a fresh clone from remote actually succeed?

```bash
( cd /tmp && rm -rf hq-probe && mkdir hq-probe && cd hq-probe
  dolt clone git+ssh://git@github.com/quickserve-ai/gt-hq-beads-v5 . 2>&1 | tail -10
  dolt log -n 1 main 2>&1
)
```
- Clone fails: remote is genuinely broken (Option B mandatory)
- Clone succeeds, HEAD differs from local: divergence — Option B with care
- Clone succeeds, HEAD matches local: corruption is in the manifest's
  history, not at HEAD — Option A may suffice

### Test 3: Was the missing blob ever pushed?

Search predecessor's recipe output (yesterday's surgical fix) and the
post-rebuild log window for `90e15fd6`:

```bash
grep -r "90e15fd6" ~/gt/.dolt-data/hq/.dolt/ 2>/dev/null | head -5
gt dolt logs -n 50000 | grep "90e15fd6" | head -5
```
- Blob present in local noms/ but missing from remote: push path silently
  dropped it during a partial-push (likely candidate)
- Blob never present anywhere: yesterday's rebuild dropped it (rebuild
  recipe needs tightening)

### Test 4: What was happening at 15:02:31 today (first occurrence)?

```bash
gt dolt logs -n 50000 | grep -B5 -A5 "2026-05-04T15:02:31" | head -40
# Look for: ongoing push, server restart, gc cycle, dolt-backup run
```

Cross-reference with the daemon log to identify what wrote to hq just
before 15:02:31.

### Decision matrix after investigation

| Finding | Action |
|---------|--------|
| Local manifest internally consistent + clone OK | Option A only |
| Local manifest references blobs not in noms/ | Option A then Option B |
| Clone fails, HEAD broken on remote | Option B mandatory |
| Blob never existed locally either | Both options + investigate write path |
| Push log around 15:02 shows partial-upload error | File a bead for the push code path before rebuilding |

## Phase 2 — Pause writes (operational)

```bash
# Stop daemon (no auto-sync churn during rebuild)
gt daemon stop

# Confirm no polecats hold hq-* assignments in flight
gt polecat list --all
( cd ~/gt/.dolt-data/hq && dolt sql -q \
  "SELECT id, assignee, status FROM issues WHERE status='in_progress' AND assignee LIKE '%/polecats/%';" )

# Mail Mayor + Deacon: read-only on hq for ~10 min
gt mail send mayor/ -s "MAINTENANCE: hq Dolt remote rebuild starting" \
  -m "Pausing daemon + polecat writes to hq for ~10 min to rebuild gt-hq-beads-v5. Authorized by Cherub. Will notify when done."
```

## Phase 3 — The repair

### Option A — Local manifest cleanup, then push (try first)

Least destructive. Works if local manifest just needs compaction.

```bash
( cd ~/gt/.dolt-data/hq
  dolt gc 2>&1 | tee "$SNAP/dolt-gc.log"
  dolt fsck 2>&1 | tee "$SNAP/dolt-fsck.log"
  dolt push origin main 2>&1 | tee "$SNAP/push-attempt-A.log"
)
```
- Push succeeds: skip Option B; go to Phase 4 verification
- Push fails with "Blob not found": Option A insufficient; proceed to Option B

### Option B — Full rebuild (predecessor's recipe + verification step)

⚠️ **Cherub authorization required for `gh api -X DELETE` and `--force` push.**

```bash
# 1. Delete the orphan remote manifest ref
gh api -X DELETE repos/quickserve-ai/gt-hq-beads-v5/git/refs/dolt/data \
  2>&1 | tee "$SNAP/gh-delete-ref.log"

# 2. Force-push from local — uploads all current blobs cleanly
( cd ~/gt/.dolt-data/hq
  dolt push origin main --force 2>&1 | tee "$SNAP/push-force.log"
)

# 3. NEW: verification step the predecessor's recipe lacked.
#    Clone fresh and confirm round-trip works before declaring success.
( cd /tmp && rm -rf hq-verify && mkdir hq-verify && cd hq-verify
  dolt clone git+ssh://git@github.com/quickserve-ai/gt-hq-beads-v5 . 2>&1 | tee "$SNAP/clone-verify.log"
  REMOTE_HEAD=$(dolt log -n 1 --oneline main | cut -d' ' -f1)
  LOCAL_HEAD=$(cd ~/gt/.dolt-data/hq && dolt log -n 1 --oneline main | cut -d' ' -f1)
  [ "$REMOTE_HEAD" = "$LOCAL_HEAD" ] && echo "✓ HEAD match" || echo "✗ HEAD MISMATCH"
  dolt sql -q "SELECT id FROM issues ORDER BY updated_at DESC LIMIT 5;" 2>&1
)
```

## Phase 4 — Verification

```bash
gt daemon start
sleep 60

# Tail logs and confirm no new "Blob not found"
gt dolt logs -n 500 2>&1 | grep -c "Blob not found"
# Expect 0 (any old occurrences from before the rebuild are fine if logs aren't rotated)

# Round-trip a real write
cd ~/gt/gastown/crew/woodhouse
TEST_ID=$(bd create --type=chore --title="rebuild verification ping $(date +%s)" --priority=P3 | grep -oE 'gt-[a-z0-9]+' | head -1)
gt dolt sync --db=hq 2>&1 | tee "$SNAP/post-rebuild-sync.log"
bd close "$TEST_ID" --reason="rebuild verification ping — closing"
```

## Phase 5 — Resume + monitor (30 min)

```bash
gt dolt status

for i in 1 2 3; do
  sleep 600
  COUNT=$(gt dolt logs -n 200 | grep -c "Blob not found")
  echo "check $i (T+$((i*10))min): $COUNT blob errors"
done
```

- 30 min clean: notify Mayor + close gt-3y4
- Recurrence: do NOT re-rebuild blindly — investigate WHO produced the new
  blob (cause is likely in the write path: a gc-during-write race, a
  partial push that silently succeeded the manifest update without the
  blob upload, or a dolt-backup interaction)

## Rollback (if Option B push fails or verification breaks something)

```bash
# Restore the deleted ref from the recorded SHA
ORIG_SHA=$(jq -r .object.sha "$SNAP/remote-refs-dolt-data.json")
gh api -X POST repos/quickserve-ai/gt-hq-beads-v5/git/refs \
  -f ref=refs/dolt/data -f sha="$ORIG_SHA"
gt mail send mayor/ -s "ESCALATION: hq rebuild rollback fired" \
  -m "Phase 3 Option B failed. Ref restored to $ORIG_SHA. Snapshot at $SNAP. Need Cherub."
```

## Open questions before executing

1. **Authorize the `gh api DELETE` + `--force` push?** Predecessor noted
   this required Cherub-auth last time.
2. **Daemon-stop window OK?** ~10 min during business hours.
3. **Investigation findings from Phase 1 may change the plan** — surface
   them to Cherub before proceeding past Phase 2.
