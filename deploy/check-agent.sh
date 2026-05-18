#!/usr/bin/env bash
# check-agent.sh — health audit for an iai K3s worker (agent) node.
#
# Run on the WORKER node (not the platform):
#     sudo /opt/it-iai/deploy/check-agent.sh
#
# Read-only. Prints one line per check: ✓ pass, ⚠ warn, ✗ fail. Exits non-zero
# if any fail. Complements deploy/check-k3s.sh which audits the platform side.
#
# What it answers, in order:
#   1. is the k3s-agent service alive at all?
#   2. has it actually joined the cluster (registered a node)?
#   3. does this node's registries.yaml route to the platform registry?
#   4. can we actually reach the platform's registry over HTTP?
#   5. are pods currently scheduled on this node? (informational)

if [[ -z "${BASH_VERSION:-}" ]]; then
  exec /usr/bin/env bash "$0" "$@"
fi

set -uo pipefail

# ────────────────────────────────────────────────────────────────────────────
# styling + counters
# ────────────────────────────────────────────────────────────────────────────

if [[ -t 1 ]]; then
  C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_FAIL=$'\033[31m'
  C_DIM=$'\033[2m'; C_BOLD=$'\033[1m'; C_RESET=$'\033[0m'
else
  C_OK="" C_WARN="" C_FAIL="" C_DIM="" C_BOLD="" C_RESET=""
fi

PASS=0; WARN=0; FAIL=0

section() { printf '\n%s── %s ──%s\n' "$C_BOLD" "$1" "$C_RESET"; }
ok()      { printf '  %s✓%s %s\n' "$C_OK"   "$C_RESET" "$1"; PASS=$((PASS+1)); }
warn()    { printf '  %s⚠%s %s\n' "$C_WARN" "$C_RESET" "$1"; WARN=$((WARN+1)); }
fail()    { printf '  %s✗%s %s\n' "$C_FAIL" "$C_RESET" "$1"; FAIL=$((FAIL+1)); }
dim()     { printf '    %s%s%s\n'   "$C_DIM"  "$1"        "$C_RESET"; }

REG_FILE=/etc/rancher/k3s/registries.yaml

# Detect this node's identity. k3s on an agent stores the joined server URL
# in /var/lib/rancher/k3s/agent/etc/server-config.yaml (newer versions) or
# can be read from systemd. Best-effort.
this_node_name="$(hostname -s 2>/dev/null || hostname)"
this_node_ip="$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -n1)"
this_node_ip="${this_node_ip:-unknown}"

# ────────────────────────────────────────────────────────────────────────────
# 1. agent service
# ────────────────────────────────────────────────────────────────────────────
section "k3s-agent service"

# Process check first — it's the authoritative signal that the agent is
# actually doing work. Systemd unit detection is informational; on some
# distros `systemctl list-unit-files` formats output in ways that defeat
# anchored greps, so we don't make it load-bearing.
AGENT_PID=$(pgrep -f '/k3s agent' 2>/dev/null | head -n1 || true)
[[ -z "$AGENT_PID" ]] && AGENT_PID=$(pgrep -f 'k3s\s\+agent' 2>/dev/null | head -n1 || true)

if [[ -n "$AGENT_PID" ]]; then
  ok "k3s agent process running (PID $AGENT_PID)"
  agent_bin=$(readlink -f /proc/"$AGENT_PID"/exe 2>/dev/null || true)
  [[ -n "$agent_bin" ]] && dim "binary: $agent_bin"
  if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet k3s-agent 2>/dev/null; then
    uptime_s=$(systemctl show -p ActiveEnterTimestamp k3s-agent --value 2>/dev/null || true)
    [[ -n "$uptime_s" ]] && dim "systemd unit active since: $uptime_s"
  fi
elif command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet k3s-agent 2>/dev/null; then
  # Systemd says active but no process — rare, usually a stale unit. Still
  # better than "is not installed" since the operator's mental model matches.
  ok "k3s-agent unit reports active (but no agent process found — restarting?)"
else
  fail "no k3s agent process running"
  dim "view logs: journalctl -u k3s-agent --no-pager -n 80"
  dim "or:        journalctl -u k3s --no-pager -n 80"
fi

# ────────────────────────────────────────────────────────────────────────────
# 2. cluster membership
# ────────────────────────────────────────────────────────────────────────────
section "cluster membership"

# k3s runs the agent + kubelet + kube-proxy + containerd inside ONE binary,
# so there is no separate `kubelet` process to look for. The single
# `/k3s agent` process IS the kubelet, which is exactly the signal we
# checked above. Re-state it as a membership signal here so the section
# stands on its own.
if [[ -n "${AGENT_PID:-}" ]]; then
  ok "kubelet (embedded in k3s agent) is up"
else
  warn "no agent process — kubelet not running"
fi

# Agent-side handshake state lives in /etc/rancher/node/password (created
# on first successful join). node-token is server-side; agents don't have it.
if [[ -s /etc/rancher/node/password ]]; then
  ok "agent join completed (handshake password cached)"
elif [[ -d /var/lib/rancher/k3s/agent ]]; then
  ok "agent state dir present: /var/lib/rancher/k3s/agent"
else
  warn "no /var/lib/rancher/k3s/agent — agent may not have joined yet"
fi

# Try to read the joined server URL.
SERVER_URL=""
for f in \
    /var/lib/rancher/k3s/agent/etc/server-config.yaml \
    /etc/systemd/system/k3s-agent.service.env
do
  if [[ -s "$f" ]]; then
    SERVER_URL=$(grep -m1 -oE 'https://[^[:space:]"]+' "$f" || true)
    [[ -n "$SERVER_URL" ]] && break
  fi
done
if [[ -n "$SERVER_URL" ]]; then
  ok "joined server: $SERVER_URL"
  # Reachability probe (best-effort; firewall might block the api but allow flannel).
  if curl -k -m 4 -sS -o /dev/null -w '%{http_code}\n' "$SERVER_URL" 2>/dev/null | grep -qE '^(200|401|403)$'; then
    ok "platform API endpoint is reachable from this node"
  else
    warn "platform API endpoint did not respond (firewall? wrong server URL?)"
  fi
else
  warn "couldn't determine joined server URL from local files"
  dim "set K3S_URL/K3S_TOKEN and re-run install-worker.sh if this node hasn't joined"
fi

# ────────────────────────────────────────────────────────────────────────────
# 3. registry mirror configuration
# ────────────────────────────────────────────────────────────────────────────
section "registry mirrors"

if [[ ! -s "$REG_FILE" ]]; then
  fail "$REG_FILE is missing or empty — workers can't pull from the platform registry"
  dim "re-run install-worker.sh with PLATFORM_IP set"
else
  ok "$REG_FILE present ($(wc -l <"$REG_FILE") lines)"
  if grep -qE '^\s*mirrors:' "$REG_FILE"; then
    ok "file declares 'mirrors:' block"
  else
    fail "no 'mirrors:' block found — containerd will not redirect pulls"
  fi
  # Surface every mirror endpoint so the operator can eyeball it.
  endpoints=$(grep -oE 'http://[^"[:space:]]+' "$REG_FILE" | sort -u | tr '\n' ' ' || true)
  if [[ -n "$endpoints" ]]; then
    dim "endpoints in file: $endpoints"
  fi
fi

# ────────────────────────────────────────────────────────────────────────────
# 4. actual registry connectivity
# ────────────────────────────────────────────────────────────────────────────
section "registry reachability"

probe_endpoint() {
  local url="$1"
  local code
  code=$(curl -m 5 -sS -o /dev/null -w '%{http_code}' "${url%/}/v2/" 2>/dev/null || echo "000")
  echo "$code"
}

# Probe each unique http://host:port endpoint declared in registries.yaml.
mapfile -t endpoints < <(grep -oE 'http://[^"[:space:]]+' "$REG_FILE" 2>/dev/null | sort -u || true)
if (( ${#endpoints[@]} == 0 )); then
  warn "nothing to probe — no http:// endpoint found in $REG_FILE"
else
  for ep in "${endpoints[@]}"; do
    code=$(probe_endpoint "$ep")
    case "$code" in
      200|401) ok "registry reachable at $ep (HTTP $code on /v2/)" ;;
      000)     fail "$ep is UNREACHABLE — security group / route blocked?" ;;
      *)       warn "$ep responded HTTP $code on /v2/ — check the platform registry" ;;
    esac
  done
fi

# ────────────────────────────────────────────────────────────────────────────
# 5. workload visibility on THIS node
# ────────────────────────────────────────────────────────────────────────────
section "workloads on this node"

# Agents don't ship with kubectl. crictl is bundled with k3s and talks to the
# local containerd, which is what we actually want — it shows the containers
# physically running on THIS node, regardless of cluster view.
if command -v k3s >/dev/null 2>&1; then
  ctrs=$(k3s crictl ps -a -o table 2>/dev/null || true)
  if [[ -z "$ctrs" ]]; then
    warn "k3s crictl returned nothing — agent may not be answering"
  else
    # Strip header, count remaining lines.
    body=$(echo "$ctrs" | tail -n +2)
    user_apps=$(echo "$body" | awk '$0 !~ /traefik|local-path|coredns|metrics-server|svclb-/' || true)
    total_lines=$(echo "$body" | grep -c . || true)
    user_lines=$(echo "$user_apps" | grep -c . || true)
    ok "containerd has $total_lines container(s) running, $user_lines user-app container(s)"
    if [[ -n "$user_apps" ]]; then
      printf '%s' "$user_apps" | awk 'NF{printf "      %s\n", $0}'
      dim "(filtered out traefik / coredns / local-path / metrics-server)"
    fi
  fi
else
  warn "k3s binary not on PATH — can't query containerd"
fi

# ────────────────────────────────────────────────────────────────────────────
# summary
# ────────────────────────────────────────────────────────────────────────────
section "summary"
printf '  %d pass · %d warn · %d fail\n' "$PASS" "$WARN" "$FAIL"
printf '  node: %s (%s)\n' "$this_node_name" "$this_node_ip"

if (( FAIL > 0 )); then
  printf '\n%s✗ %d failure(s) — fix these before this worker can serve pods.%s\n' "$C_FAIL" "$FAIL" "$C_RESET"
  exit 1
fi
if (( WARN > 0 )); then
  printf '\n%s⚠ %d warning(s) — likely cosmetic but worth a look.%s\n' "$C_WARN" "$WARN" "$C_RESET"
fi
printf '\n%s✓ worker looks healthy.%s\n' "$C_OK" "$C_RESET"
exit 0
