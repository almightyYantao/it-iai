#!/usr/bin/env bash
# check-k3s.sh — comprehensive K3s + iai environment audit.
#
# Run on the PLATFORM node:
#     sudo /opt/iai/deploy/check-k3s.sh
#
# Prints one line per check: ✓ pass, ⚠ warning, ✗ fail. Exits non-zero if any
# fail. Safe to run repeatedly — read-only, no mutation.

# Guard against being invoked via `sh check-k3s.sh` — RHEL's /bin/sh is dash,
# which doesn't interpret \033 escapes, so colors render as literal junk.
if [[ -z "${BASH_VERSION:-}" ]]; then
  exec /usr/bin/env bash "$0" "$@"
fi

# We don't `set -e` because individual checks must continue past failures
# to produce a complete report.
set -uo pipefail

# ────────────────────────────────────────────────────────────────────────────
# Colors / counters / helpers
# ────────────────────────────────────────────────────────────────────────────

if [[ -t 1 ]]; then
  # ANSI-C quoting ($'…') puts the ACTUAL ESC byte in the variable.
  # Plain "\033[32m" stores the literal 6-character string and prints as junk.
  C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_FAIL=$'\033[31m'; C_DIM=$'\033[2m'
  C_BOLD=$'\033[1m'; C_RESET=$'\033[0m'
else
  C_OK=""; C_WARN=""; C_FAIL=""; C_DIM=""; C_BOLD=""; C_RESET=""
fi

PASS=0; WARN=0; FAIL=0

section() { printf '\n%s── %s ──%s\n' "$C_BOLD" "$1" "$C_RESET"; }
ok()      { printf '  %s✓%s %s\n' "$C_OK"   "$C_RESET" "$1"; PASS=$((PASS+1)); }
warn()    { printf '  %s⚠%s %s\n' "$C_WARN" "$C_RESET" "$1"; WARN=$((WARN+1)); }
fail()    { printf '  %s✗%s %s\n' "$C_FAIL" "$C_RESET" "$1"; FAIL=$((FAIL+1)); }
dim()     { printf '    %s%s%s\n'   "$C_DIM"  "$1"        "$C_RESET"; }

# Run a command but capture its output for use in messages. Returns exit code.
cap() { CAP_OUT="$($* 2>&1)"; CAP_EC=$?; return $CAP_EC; }

# Where the repo lives. Default mirrors install-platform.sh's INSTALL_DIR.
# Try the canonical /opt/it-iai first, then the older /opt/iai and the original
# /opt/vibedeploy as fallbacks so this audit works against any pre-rename install.
REPO_ROOT="${REPO_ROOT:-/opt/it-iai}"
[[ -d "$REPO_ROOT" ]] || REPO_ROOT="/opt/iai"
[[ -d "$REPO_ROOT" ]] || REPO_ROOT="/opt/vibedeploy"
ENV_FILE="$REPO_ROOT/.env"

# k3s installs to /usr/local/bin, which sudo's secure_path skips on RHEL.
# Search explicit locations before giving up — otherwise the script falls
# back to system `kubectl` silently and every cluster check fails with no
# kubeconfig.
KUBECTL=()
for c in k3s /usr/local/bin/k3s /usr/bin/k3s; do
  if [[ "$c" == */* ]]; then
    [[ -x "$c" ]] || continue
  else
    command -v "$c" >/dev/null 2>&1 || continue
  fi
  KUBECTL=("$c" kubectl)
  break
done
if [[ ${#KUBECTL[@]} -eq 0 ]]; then
  for c in kubectl /usr/local/bin/kubectl /usr/bin/kubectl; do
    if [[ "$c" == */* ]]; then
      [[ -x "$c" ]] || continue
    else
      command -v "$c" >/dev/null 2>&1 || continue
    fi
    KUBECTL=("$c")
    [[ -z "${KUBECONFIG:-}" && -r /etc/rancher/k3s/k3s.yaml ]] && export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
    break
  done
fi
# Empty KUBECTL leaves checks below to fail individually — preferable to
# aborting the whole audit on a missing client.

# ────────────────────────────────────────────────────────────────────────────
# 1. K3s service
# ────────────────────────────────────────────────────────────────────────────

section "K3s server"

if systemctl is-active --quiet k3s 2>/dev/null; then
  ok "k3s.service is active"
else
  fail "k3s.service is NOT active"
  dim "$(systemctl status k3s --no-pager --lines=3 2>&1 | tail -3)"
fi

# Reading the actual ExecStart catches a misconfigured systemd unit
# (we've been bitten by stale --disable=servicelb flags here).
if [[ -f /etc/systemd/system/k3s.service ]]; then
  EXEC_LINE=$(grep -A20 '^ExecStart=' /etc/systemd/system/k3s.service | tr -d '\\\n' | tr -s ' ')
  if [[ "$EXEC_LINE" == *"--disable=servicelb"* ]] && [[ "$EXEC_LINE" == *"--disable=traefik"* ]]; then
    warn "K3s started with --disable=traefik AND --disable=servicelb — there's no bundled ingress"
  fi
  dim "ExecStart args: $(echo "$EXEC_LINE" | sed -E 's|.*ExecStart=/usr/local/bin/k3s ||' | cut -c1-200)"
fi

# ────────────────────────────────────────────────────────────────────────────
# 2. Nodes
# ────────────────────────────────────────────────────────────────────────────

section "Cluster nodes"

if cap "${KUBECTL[@]}" get nodes --no-headers; then
  TOTAL_NODES=0; READY_NODES=0
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    TOTAL_NODES=$((TOTAL_NODES+1))
    name=$(awk '{print $1}' <<<"$line")
    status=$(awk '{print $2}' <<<"$line")
    role=$(awk '{print $3}' <<<"$line")
    version=$(awk '{print $5}' <<<"$line")
    if [[ "$status" == "Ready" ]]; then
      READY_NODES=$((READY_NODES+1))
      ok "${name} (${role}) — Ready · ${version}"
    else
      fail "${name} (${role}) — ${status}"
    fi
  done <<<"$CAP_OUT"
  if (( READY_NODES == 0 )); then
    fail "no Ready nodes — control plane / agents not joined"
  elif (( READY_NODES < TOTAL_NODES )); then
    warn "${READY_NODES}/${TOTAL_NODES} nodes Ready"
  fi
else
  fail "kubectl get nodes failed"
  dim "$CAP_OUT"
fi

# ────────────────────────────────────────────────────────────────────────────
# 3. Traefik
# ────────────────────────────────────────────────────────────────────────────

section "Traefik (ingress)"

if cap "${KUBECTL[@]}" -n kube-system get deploy traefik -o jsonpath='{.status.readyReplicas}/{.status.replicas}'; then
  ready_repl="$CAP_OUT"
  if [[ "$ready_repl" =~ ^([0-9]+)/([0-9]+)$ ]]; then
    r="${BASH_REMATCH[1]}"; total="${BASH_REMATCH[2]}"
    if (( r > 0 )) && (( r == total )); then
      ok "traefik Deployment Ready ${ready_repl}"
    else
      fail "traefik Deployment ${ready_repl} — pods not Ready"
    fi
  else
    fail "could not parse traefik replica status: '$ready_repl'"
  fi
else
  # Maybe it's a DaemonSet (HelmChartConfig switched it)
  if cap "${KUBECTL[@]}" -n kube-system get ds traefik -o jsonpath='{.status.numberReady}/{.status.desiredNumberScheduled}'; then
    ok "traefik DaemonSet Ready $CAP_OUT"
  else
    fail "traefik not found as Deployment OR DaemonSet"
  fi
fi

# hostNetwork? Required for traefik pod to bind :80 / :443 without servicelb.
if cap "${KUBECTL[@]}" -n kube-system get pods -l app.kubernetes.io/name=traefik -o jsonpath='{.items[0].spec.hostNetwork}'; then
  if [[ "$CAP_OUT" == "true" ]]; then
    ok "traefik pod runs with hostNetwork=true"
  else
    warn "traefik pod NOT on hostNetwork — relies on servicelb / external LB for :80/:443"
  fi
fi

# Is :80 / :443 actually bound on this host?
LISTEN=$(ss -tlnp 2>/dev/null | grep -E ':80\b|:443\b' || true)
if [[ -n "$LISTEN" ]]; then
  ok "host :80 / :443 are bound"
  while IFS= read -r line; do
    proc=$(echo "$line" | sed -E 's/.*users:\(\("([^"]+)".*$/\1/')
    port=$(echo "$line" | awk '{print $4}')
    dim "  $port  ←  $proc"
  done <<<"$LISTEN"
else
  fail "nothing listening on :80 / :443 — Traefik isn't reachable externally"
fi

# ────────────────────────────────────────────────────────────────────────────
# 4. Registry: docker daemon + containerd mirror config
# ────────────────────────────────────────────────────────────────────────────

section "Image registry"

# 4a. Local registry container reachable?
REG_PORT="${REGISTRY_PORT:-5001}"
if cap curl -sfS -m 3 "http://127.0.0.1:${REG_PORT}/v2/"; then
  ok "registry at 127.0.0.1:${REG_PORT} responds to /v2/"
else
  fail "registry at 127.0.0.1:${REG_PORT} unreachable"
  dim "is the compose stack running? docker compose ps registry"
fi

# 4b. registries.yaml present + matches .env
REG_YAML=/etc/rancher/k3s/registries.yaml
if [[ -f "$REG_YAML" ]]; then
  ok "/etc/rancher/k3s/registries.yaml present"
  dim "  mirrors: $(grep -E '^[[:space:]]+"' "$REG_YAML" | tr -s ' ' | tr -d '"' | head -3 | tr '\n' ',' | sed 's/,$//')"

  # Cross-check: the host the env says we tag with must be in registries.yaml.
  if [[ -f "$ENV_FILE" ]]; then
    pull_host=$(grep -E '^CP_REGISTRY_HOST_FROM_K3D=' "$ENV_FILE" | sed -E 's|.*=||' || true)
    push_host=$(grep -E '^CP_REGISTRY_HOST=' "$ENV_FILE" | sed -E 's|.*=||' || true)
    if [[ -n "$pull_host" ]]; then
      if grep -qF "\"$pull_host\"" "$REG_YAML"; then
        ok "CP_REGISTRY_HOST_FROM_K3D=${pull_host} is mirrored"
      else
        fail "CP_REGISTRY_HOST_FROM_K3D=${pull_host} is NOT in registries.yaml — containerd can't pull"
      fi
    fi
    [[ -n "$push_host" ]] && dim "  push host: ${push_host} (docker daemon side)"
  fi
else
  fail "$REG_YAML missing — containerd doesn't know the registry"
fi

# 4c. Docker daemon HTTP-OK for the loopback registry.
# `docker info` prints insecure registries one per line under a section header
# — match on the content line itself (multiline grep with -E AB doesn't work
# across line breaks).
if cap docker info; then
  if echo "$CAP_OUT" | grep -qF '127.0.0.0/8'; then
    ok "docker daemon trusts 127.0.0.0/8 (loopback push works without TLS config)"
  else
    warn "docker daemon doesn't list 127.0.0.0/8 as insecure — push to 127.0.0.1:${REG_PORT} may need /etc/docker/daemon.json"
  fi
fi

# ────────────────────────────────────────────────────────────────────────────
# 5. Kubeconfig for control-plane container
# ────────────────────────────────────────────────────────────────────────────

section "Control-plane → K3s wiring"

KUBE_FILE="$REPO_ROOT/kube/config"
if [[ -f "$KUBE_FILE" ]]; then
  ok "$KUBE_FILE exists"
  if grep -q 'host.docker.internal' "$KUBE_FILE"; then
    ok "kubeconfig server URL points at host.docker.internal (container-reachable)"
  else
    warn "kubeconfig still points at 127.0.0.1 — control-plane container can't reach K3s"
  fi
  if grep -q 'insecure-skip-tls-verify: true' "$KUBE_FILE"; then
    ok "insecure-skip-tls-verify enabled (K3s self-signed cert SAN won't fight us)"
  else
    warn "no insecure-skip-tls-verify — TLS hostname-mismatch errors likely"
  fi
else
  fail "$KUBE_FILE missing — control-plane container has no kubeconfig"
  dim "run: sudo deploy/migrate-registry-host.sh   (or re-run install-platform.sh)"
fi

# Control-plane container actually healthy?
if cap curl -fsS -m 3 http://127.0.0.1:8080/healthz; then
  ok "control-plane /healthz: $CAP_OUT"
else
  fail "control-plane /healthz unreachable on 127.0.0.1:8080"
fi

# Did the K8s reconciler actually start? Greppable from its first-tick log line.
if cap docker compose -f "$REPO_ROOT/docker-compose.yml" logs control-plane --tail=200; then
  if echo "$CAP_OUT" | grep -q 'k8s driver disabled'; then
    fail "control-plane logs say 'k8s driver disabled' — reconciler is NOT running"
    dim "$(echo "$CAP_OUT" | grep 'k8s driver disabled' | tail -1)"
  else
    ok "control-plane shows no 'k8s driver disabled' message"
  fi
fi

# ────────────────────────────────────────────────────────────────────────────
# 6. Live workloads — any iai projects deployed?
# ────────────────────────────────────────────────────────────────────────────

section "iai workloads"

if cap "${KUBECTL[@]}" get ns -l vibedeploy.io/managed=true -o jsonpath='{.items[*].metadata.name}'; then
  ns_list="$CAP_OUT"
  if [[ -z "$ns_list" ]]; then
    dim "no project namespaces yet — push something with `deploy +push` to populate this section"
  else
    proj_count=0
    for ns in $ns_list; do
      proj_count=$((proj_count+1))
      ready=$("${KUBECTL[@]}" -n "$ns" get pods --no-headers 2>/dev/null | awk '{r=$2; status=$3; print r " " status}' | head -1)
      if [[ "$ready" =~ ^([0-9]+/[0-9]+)\ +Running$ ]]; then
        ok "${ns} — ${ready}"
      elif [[ -n "$ready" ]]; then
        warn "${ns} — ${ready}"
      else
        warn "${ns} — no pods (deployment may be pending)"
      fi
    done
    ok "found ${proj_count} project namespace(s)"
  fi
fi

# ────────────────────────────────────────────────────────────────────────────
# 7. DNS / inbound check
# ────────────────────────────────────────────────────────────────────────────

section "Inbound HTTP"

BASE_DOMAIN=""
if [[ -f "$ENV_FILE" ]]; then
  BASE_DOMAIN=$(grep -E '^CP_APP_BASE_DOMAIN=' "$ENV_FILE" | sed -E 's|.*=||' || true)
fi
[[ -n "$BASE_DOMAIN" ]] && dim "app base domain: $BASE_DOMAIN"

# Always probe :80, with a synthetic host if BASE_DOMAIN is unset. The point
# of the check is to confirm Traefik is bound and routing — not to test a real
# host. Connection-refused = no ingress; 404 from Traefik = working.
probe_host="doesnotexist.${BASE_DOMAIN:-iai.local}"
if cap curl -sS -o /dev/null -w '%{http_code}' -m 5 -H "Host: $probe_host" "http://127.0.0.1/"; then
  code="$CAP_OUT"
  case "$code" in
    404|421)             ok   "Traefik routes :80 traffic (got ${code} for unknown host — expected)" ;;
    200|3??)             ok   "Traefik :80 responds (${code})" ;;
    000)                 fail "couldn't connect to 127.0.0.1:80 — nothing listening" ;;
    *)                   warn "Traefik :80 returned HTTP ${code} for unknown host" ;;
  esac
else
  fail "couldn't reach 127.0.0.1:80 with curl"
fi

# ────────────────────────────────────────────────────────────────────────────
# Summary
# ────────────────────────────────────────────────────────────────────────────

printf '\n%s── Summary ──%s\n' "$C_BOLD" "$C_RESET"
printf '  %s%d passed%s · %s%d warnings%s · %s%d failed%s\n\n' \
  "$C_OK" "$PASS" "$C_RESET" \
  "$C_WARN" "$WARN" "$C_RESET" \
  "$C_FAIL" "$FAIL" "$C_RESET"

if (( FAIL > 0 )); then
  exit 1
elif (( WARN > 0 )); then
  exit 0  # warnings don't fail CI — they're informational
fi
exit 0
