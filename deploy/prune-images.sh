#!/usr/bin/env bash
# prune-images.sh — reclaim disk from build leftovers, on a schedule.
#
# Run on the PLATFORM node, from cron:
#     0 4 * * 0 /opt/it-iai/deploy/prune-images.sh >> /var/log/it-iai-prune.log 2>&1
#
# Why this exists: every deploy runs nixpacks, which leaves behind an image and
# a pile of build cache. Nothing ever removed them, so the node accumulated
# 1296 images (127 GB) and 24 GB of build cache until the disk crossed
# kubelet's image-GC threshold. The GC then deleted images as fast as it could,
# and the I/O storm took kine (k3s's SQLite datastore) down with it — the API
# server was unusable for 14 hours. Cheap periodic cleanup prevents the whole
# chain.
#
# Safe to run repeatedly. Never touches volumes, so Postgres / MinIO / Redis /
# registry data is out of scope by construction.

if [[ -z "${BASH_VERSION:-}" ]]; then
  exec /usr/bin/env bash "$0" "$@"
fi
set -uo pipefail

# Images and cache younger than this are kept so routine rebuilds still hit
# warm layers. A week covers the normal iterate-on-a-project rhythm.
KEEP_WINDOW="${KEEP_WINDOW:-168h}"

# Above this, the retention window is abandoned and everything unused goes.
# Set below kubelet's image-GC high-water mark (85%) so this script wins the
# race and the GC never has to run.
PANIC_PCT="${PANIC_PCT:-70}"

log() { printf '%s %s\n' "$(date -Is)" "$*"; }
disk_pct() { df --output=pcent / | tail -1 | tr -dc '0-9'; }

before_pct=$(disk_pct)
before_avail=$(df -h --output=avail / | tail -1 | tr -d ' ')
log "start — disk ${before_pct}% used, ${before_avail} free"

if (( before_pct >= PANIC_PCT )); then
  log "disk at ${before_pct}% (>= ${PANIC_PCT}%) — dropping the retention window"
  docker builder prune -af 2>&1 | tail -1
  docker image prune -af 2>&1 | tail -1
else
  docker builder prune -af --filter "until=${KEEP_WINDOW}" 2>&1 | tail -1
  docker image prune -af --filter "until=${KEEP_WINDOW}" 2>&1 | tail -1
fi

# Exited containers from failed builds. Running containers are never eligible.
docker container prune -f 2>&1 | tail -1

after_pct=$(disk_pct)
after_avail=$(df -h --output=avail / | tail -1 | tr -d ' ')
log "done  — disk ${after_pct}% used, ${after_avail} free (was ${before_pct}%)"

# Loud if cleanup wasn't enough; whatever reads this log can alert on it.
if (( after_pct >= 80 )); then
  log "WARNING: still at ${after_pct}% after pruning — something other than images is growing"
  exit 1
fi
