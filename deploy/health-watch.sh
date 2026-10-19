#!/usr/bin/env bash
# health-watch.sh — the checks that would have caught the 2026-10-18 outage.
#
# Run on the PLATFORM node, from cron:
#     */10 * * * * /opt/it-iai/deploy/health-watch.sh >> /var/log/it-iai-health.log 2>&1
#
# Each check here maps to something that actually broke and was noticed only
# because a person went looking:
#
#   disk      — filled to 85%, tripped kubelet's image GC, the I/O storm killed
#               kine and the API server was dead for 14 hours.
#   api       — "k3s is running" stayed true the whole time. The process lives
#               while serving nothing, so liveness has to be a real API call.
#   kine      — the specific corpse: the datastore socket disappears and every
#               request hangs. Cheap to check, unambiguous when it fires.
#   certs     — per-project certs silently stopped renewing in August; one host
#               served an expired cert for seven weeks.
#   wildcard  — *.iai.longbridge-inc.com is installed by hand via install-tls.sh
#               and renewed by nobody. It has no cert-manager object watching
#               it, so it needs its own expiry check.
#
# Exits 0 when everything passes, 1 when anything fails. Set ALERT_WEBHOOK to
# have failures posted somewhere; without it the log is the only output and
# something else has to watch the exit code.

if [[ -z "${BASH_VERSION:-}" ]]; then
  exec /usr/bin/env bash "$0" "$@"
fi
# No `set -e`: every check must run so one report covers the whole system.
set -uo pipefail

export PATH="$PATH:/usr/local/bin"
KUBECTL=(k3s kubectl)

DISK_WARN_PCT="${DISK_WARN_PCT:-75}"
CERT_WARN_DAYS="${CERT_WARN_DAYS:-14}"
WILDCARD_SECRET="${WILDCARD_SECRET:-iai-wildcard-tls}"
WILDCARD_NS="${WILDCARD_NS:-kube-system}"
ALERT_WEBHOOK="${ALERT_WEBHOOK:-}"

PROBLEMS=()
note_problem() { PROBLEMS+=("$1"); echo "FAIL  $1"; }
note_ok()      { echo "ok    $1"; }

# ── disk ────────────────────────────────────────────────────────────────────
pct=$(df --output=pcent / | tail -1 | tr -dc '0-9')
if (( pct >= DISK_WARN_PCT )); then
  note_problem "disk at ${pct}% (threshold ${DISK_WARN_PCT}%) — kubelet image GC fires at 85%"
else
  note_ok "disk ${pct}%"
fi

# ── api server ──────────────────────────────────────────────────────────────
# A real request, not `systemctl is-active`: the k3s process stayed "active"
# through the entire outage while answering nothing.
if "${KUBECTL[@]}" get --raw='/readyz' --request-timeout=10s >/dev/null 2>&1; then
  note_ok "api server responding"
else
  note_problem "api server not responding within 10s"
fi

# ── kine datastore socket ───────────────────────────────────────────────────
if [[ -S /var/lib/rancher/k3s/server/kine.sock ]]; then
  note_ok "kine.sock present"
else
  note_problem "kine.sock missing — datastore is down, restart k3s"
fi

# ── nodes ───────────────────────────────────────────────────────────────────
not_ready=$("${KUBECTL[@]}" get nodes --no-headers --request-timeout=10s 2>/dev/null \
            | awk '$2!="Ready"{print $1}' | tr '\n' ' ')
if [[ -n "$not_ready" ]]; then
  note_problem "nodes not Ready: ${not_ready}"
else
  note_ok "all nodes Ready"
fi

# ── cert-manager certificates ───────────────────────────────────────────────
bad_certs=$("${KUBECTL[@]}" get certificate -A --no-headers --request-timeout=15s 2>/dev/null \
            | awk '$3!="True"{print $1"/"$2}' | tr '\n' ' ')
if [[ -n "$bad_certs" ]]; then
  note_problem "certificates not Ready: ${bad_certs}"
else
  note_ok "all certificates Ready"
fi

# ── the hand-managed wildcard ───────────────────────────────────────────────
wc_crt=$("${KUBECTL[@]}" -n "$WILDCARD_NS" get secret "$WILDCARD_SECRET" \
         -o jsonpath='{.data.tls\.crt}' --request-timeout=10s 2>/dev/null | base64 -d 2>/dev/null)
if [[ -z "$wc_crt" ]]; then
  note_problem "wildcard secret ${WILDCARD_NS}/${WILDCARD_SECRET} not readable"
else
  end_date=$(echo "$wc_crt" | openssl x509 -noout -enddate 2>/dev/null | cut -d= -f2)
  end_epoch=$(date -d "$end_date" +%s 2>/dev/null || echo 0)
  days_left=$(( (end_epoch - $(date +%s)) / 86400 ))
  if (( end_epoch == 0 )); then
    note_problem "wildcard cert expiry unparseable"
  elif (( days_left < CERT_WARN_DAYS )); then
    note_problem "wildcard cert expires in ${days_left}d (${end_date}) — renewed by hand, nothing will do it for you"
  else
    note_ok "wildcard cert ${days_left}d left"
  fi
fi

# ── report ──────────────────────────────────────────────────────────────────
if (( ${#PROBLEMS[@]} == 0 )); then
  echo "$(date -Is) all checks passed"
  exit 0
fi

echo "$(date -Is) ${#PROBLEMS[@]} problem(s)"
if [[ -n "$ALERT_WEBHOOK" ]]; then
  text="it-iai health check: ${#PROBLEMS[@]} problem(s)"$'\n'"$(printf '• %s\n' "${PROBLEMS[@]}")"
  # Lark/Slack-compatible minimal payload. jq isn't guaranteed on the node, so
  # the text is escaped by hand — keep it to plain ASCII punctuation.
  escaped=${text//\\/\\\\}; escaped=${escaped//\"/\\\"}; escaped=${escaped//$'\n'/\\n}
  curl -s -m 10 -X POST "$ALERT_WEBHOOK" \
       -H 'Content-Type: application/json' \
       -d "{\"msg_type\":\"text\",\"content\":{\"text\":\"${escaped}\"}}" >/dev/null 2>&1 \
    || echo "WARN  webhook post failed"
fi
exit 1
