#!/usr/bin/env bash
# dolt-hq-rebuild-forensics.sh — Read-only evidence capture for gt-3y4.
#
# Captures Phase 0 (forensics) + Phase 1 (root-cause investigation) for
# the dolt hq remote rebuild runbook. Writes everything to a timestamped
# snapshot directory. NO destructive operations.
#
# Runbook: docs/design/dolt-hq-remote-rebuild.md
# Bead: gt-3y4
#
# Usage: scripts/dolt-hq-rebuild-forensics.sh

set -uo pipefail

SNAP="/tmp/dolt-rebuild-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$SNAP"

DOLT_DATA="${DOLT_DATA_DIR:-$HOME/gt/.dolt-data}"
HQ_DIR="$DOLT_DATA/hq"
REMOTE_REPO="quickserve-ai/gt-hq-beads-v5"
REMOTE_URL="git+ssh://git@github.com/${REMOTE_REPO}"

log() { echo "[forensics] $*"; }

if [ ! -d "$HQ_DIR/.dolt" ]; then
  log "ERROR: hq Dolt directory not found at $HQ_DIR"
  exit 1
fi

log "Snapshot dir: $SNAP"

# --- Phase 0: server + log + local + remote state ----------------------------

log "Capturing dolt server status..."
gt dolt status > "$SNAP/dolt-status.txt" 2>&1 || true

log "Capturing dolt logs (last 20000 lines)..."
gt dolt logs -n 20000 > "$SNAP/dolt-logs.txt" 2>&1 || true

log "Capturing local hq state..."
( cd "$HQ_DIR"
  dolt log -n 20 --oneline > "$SNAP/hq-local-log.txt" 2>&1 || true
  dolt log -n 1 main       > "$SNAP/hq-local-head.txt" 2>&1 || true
  dolt remote -v           > "$SNAP/hq-remotes.txt"    2>&1 || true
  ls -la .dolt/noms/       > "$SNAP/hq-noms.txt"       2>&1 || true
  if [ -f .dolt/manifest ]; then
    cp .dolt/manifest "$SNAP/hq-manifest.bin"
    file .dolt/manifest > "$SNAP/hq-manifest-info.txt" 2>&1 || true
  fi
)

log "Capturing remote ref state via gh api..."
if command -v gh >/dev/null 2>&1; then
  gh api "repos/${REMOTE_REPO}/git/refs/dolt/data" \
    > "$SNAP/remote-refs-dolt-data.json" 2>&1 || true
  gh api "repos/${REMOTE_REPO}/branches/main" \
    > "$SNAP/remote-main.json" 2>&1 || true
else
  log "WARN: gh not found; skipping remote ref capture"
fi

log "Extracting distinct missing blob hashes..."
grep "Blob not found" "$SNAP/dolt-logs.txt" 2>/dev/null \
  | awk -F'Blob not found: ' '{print $2}' \
  | awk -F'"' '{print $1}' \
  | sort -u > "$SNAP/missing-blobs.txt"

log "Counting error frequency by minute (last 1 hour)..."
grep "Blob not found" "$SNAP/dolt-logs.txt" 2>/dev/null \
  | awk -F'time="' '{print substr($2,1,16)}' \
  | awk -F'"' '{print $1}' \
  | sort | uniq -c \
  | tail -60 > "$SNAP/blob-errors-by-minute.txt"

# --- Phase 1: root-cause investigation ---------------------------------------

log "Test 1: local manifest consistency (dolt admin storage)..."
( cd "$HQ_DIR"
  dolt admin storage 2>&1 | head -50 > "$SNAP/test1-admin-storage.txt" || \
    echo "dolt admin storage not available in this dolt version" \
    > "$SNAP/test1-admin-storage.txt"
)

log "Test 2: probe remote with a fresh clone (read-only)..."
PROBE_DIR="$SNAP/test2-clone-probe"
mkdir -p "$PROBE_DIR"

# Pick a timeout binary; fall back to running unbounded if neither exists.
TIMEOUT_CMD=""
if command -v gtimeout >/dev/null 2>&1; then
  TIMEOUT_CMD="gtimeout 60"
elif command -v timeout >/dev/null 2>&1; then
  TIMEOUT_CMD="timeout 60"
fi

( cd "$PROBE_DIR"
  $TIMEOUT_CMD dolt clone "$REMOTE_URL" . > "$SNAP/test2-clone.log" 2>&1
  RC=$?
  if [ $RC -eq 0 ]; then
    dolt log -n 1 main > "$SNAP/test2-remote-head.txt" 2>&1 || true
    REMOTE_HEAD=$(dolt log -n 1 --oneline main 2>/dev/null | cut -d' ' -f1)
    LOCAL_HEAD=$(cd "$HQ_DIR" && dolt log -n 1 --oneline main 2>/dev/null | cut -d' ' -f1)
    {
      echo "remote HEAD: $REMOTE_HEAD"
      echo "local HEAD:  $LOCAL_HEAD"
      if [ "$REMOTE_HEAD" = "$LOCAL_HEAD" ]; then
        echo "MATCH — corruption is in history, not at HEAD"
      else
        echo "DIFFER — divergence requires care"
      fi
    } > "$SNAP/test2-head-comparison.txt"
  else
    echo "Clone failed (rc=$RC) — remote is broken; Option B mandatory" \
      > "$SNAP/test2-head-comparison.txt"
  fi
)

log "Test 3: search for missing blob locally..."
{
  echo "=== Missing blobs from logs ==="
  cat "$SNAP/missing-blobs.txt"
  echo
  echo "=== grep .dolt/ for each blob hash ==="
  while read -r BLOB; do
    [ -z "$BLOB" ] && continue
    HASH_PREFIX=$(echo "$BLOB" | cut -d'.' -f1 | head -c 12)
    echo
    echo "--- $BLOB ---"
    if [ -f "$HQ_DIR/.dolt/noms/$BLOB" ]; then
      echo "PRESENT in local noms/"
      ls -la "$HQ_DIR/.dolt/noms/$BLOB"
    else
      echo "ABSENT from local noms/"
    fi
    echo "Log occurrences:"
    grep -c "$HASH_PREFIX" "$SNAP/dolt-logs.txt" 2>/dev/null || echo "0"
  done < "$SNAP/missing-blobs.txt"
} > "$SNAP/test3-blob-presence.txt"

log "Test 4: context around first error occurrence..."
FIRST_ERR_TIME=$(grep "Blob not found" "$SNAP/dolt-logs.txt" 2>/dev/null \
  | head -1 | awk -F'time="' '{print substr($2,1,19)}' | awk -F'"' '{print $1}')
if [ -n "$FIRST_ERR_TIME" ]; then
  {
    echo "First blob error at: $FIRST_ERR_TIME"
    echo
    echo "=== 10 lines before/after first occurrence ==="
    grep -B10 -A10 "$FIRST_ERR_TIME" "$SNAP/dolt-logs.txt" 2>/dev/null | head -50
  } > "$SNAP/test4-first-error-context.txt"
else
  echo "No blob errors found in log window" > "$SNAP/test4-first-error-context.txt"
fi

# --- Summary -----------------------------------------------------------------

log "Building summary..."
{
  echo "=== Dolt hq Rebuild Forensics Summary ==="
  echo "Snapshot: $SNAP"
  echo "Captured: $(date)"
  echo
  echo "=== Server status ==="
  head -20 "$SNAP/dolt-status.txt"
  echo
  echo "=== Local HEAD ==="
  head -5 "$SNAP/hq-local-head.txt"
  echo
  echo "=== Distinct missing blobs ==="
  cat "$SNAP/missing-blobs.txt"
  echo
  echo "=== Total blob errors in log window ==="
  grep -c "Blob not found" "$SNAP/dolt-logs.txt" 2>/dev/null || echo 0
  echo
  echo "=== Test 2: clone probe verdict ==="
  cat "$SNAP/test2-head-comparison.txt"
  echo
  echo "=== Test 3: blob presence ==="
  cat "$SNAP/test3-blob-presence.txt"
  echo
  echo "=== Test 4: first error context ==="
  head -20 "$SNAP/test4-first-error-context.txt"
  echo
  echo "=== Files captured ==="
  ls -la "$SNAP/"
} > "$SNAP/SUMMARY.txt"

log "Done."
log "Summary: $SNAP/SUMMARY.txt"
log ""
log "Next steps (do not run yet):"
log "  1. Review $SNAP/SUMMARY.txt"
log "  2. Use the decision matrix in docs/design/dolt-hq-remote-rebuild.md"
log "  3. Get Cherub auth before Phase 2+"

cat "$SNAP/SUMMARY.txt"
