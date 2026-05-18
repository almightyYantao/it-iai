#!/usr/bin/env bash
# Patch an existing /opt/it-iai/.env produced by an earlier install-platform.sh
# to the current registry-hostname convention. Idempotent.
#
# Run from repo root on the platform node:
#   sudo deploy/migrate-registry-host.sh
#
# Env overrides:
#   REGISTRY_PULL_HOST  the host:port K3s nodes (workers especially) will use
#                       to pull images. Default ${PLATFORM_IP}:5001. Override
#                       when workers are on a different network than the
#                       platform's private IP, e.g.:
#                         sudo REGISTRY_PULL_HOST=<public-ip>:5001 deploy/...
#
# Effect:
#   CP_REGISTRY_HOST          → 127.0.0.1:5001                (push side)
#   CP_REGISTRY_HOST_FROM_K3D → \${REGISTRY_PULL_HOST}         (K3s pull side)
#   /etc/rancher/k3s/registries.yaml  ← add a mirror entry for that host
#
# Then restart the affected services:
#   docker compose up -d --build control-plane build-service

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
# shellcheck source=lib/_common.sh
source "$HERE/lib/_common.sh"

ENV_FILE="$REPO_ROOT/.env"
[[ -f "$ENV_FILE" ]] || vd_die "$ENV_FILE not found — run deploy/install-platform.sh first"

REGISTRY_PORT="${REGISTRY_PORT:-5001}"
PLATFORM_IP="${PLATFORM_IP:-$(vd_detect_ip)}"
[[ -n "$PLATFORM_IP" ]] || vd_die "could not detect platform IP; set PLATFORM_IP=..."
REGISTRY_PULL_HOST="${REGISTRY_PULL_HOST:-${PLATFORM_IP}:${REGISTRY_PORT}}"

vd_info "patching $ENV_FILE"
vd_info "  CP_REGISTRY_HOST          = 127.0.0.1:${REGISTRY_PORT}"
vd_info "  CP_REGISTRY_HOST_FROM_K3D = ${REGISTRY_PULL_HOST}"

# Take a backup the first time we touch it; never overwrite the backup.
[[ -f "${ENV_FILE}.bak" ]] || cp "$ENV_FILE" "${ENV_FILE}.bak"

# Replace the two lines (works whether they had host.docker.internal, the old
# platform IP, or something else entirely).
sed -i.tmp -E \
  -e "s|^CP_REGISTRY_HOST=.*|CP_REGISTRY_HOST=127.0.0.1:${REGISTRY_PORT}|" \
  -e "s|^CP_REGISTRY_HOST_FROM_K3D=.*|CP_REGISTRY_HOST_FROM_K3D=${REGISTRY_PULL_HOST}|" \
  "$ENV_FILE"
rm -f "${ENV_FILE}.tmp"
vd_ok "patched .env (backup at ${ENV_FILE}.bak)"

# Also rewrite the platform's own k3s registries.yaml so its containerd recognises
# the new pull host. Adds a mirror entry that routes to 127.0.0.1:${REGISTRY_PORT}.
REG_FILE=/etc/rancher/k3s/registries.yaml
if [[ -f /etc/rancher/k3s/k3s.yaml ]]; then
  vd_info "rewriting $REG_FILE with mirror for ${REGISTRY_PULL_HOST}"
  cat > "$REG_FILE" <<EOF
mirrors:
  "host.docker.internal:${REGISTRY_PORT}":
    endpoint:
      - "http://127.0.0.1:${REGISTRY_PORT}"
  "${PLATFORM_IP}:${REGISTRY_PORT}":
    endpoint:
      - "http://127.0.0.1:${REGISTRY_PORT}"
  "${REGISTRY_PULL_HOST}":
    endpoint:
      - "http://127.0.0.1:${REGISTRY_PORT}"
configs:
  "127.0.0.1:${REGISTRY_PORT}":
    tls: { insecure_skip_verify: true }
  "host.docker.internal:${REGISTRY_PORT}":
    tls: { insecure_skip_verify: true }
  "${PLATFORM_IP}:${REGISTRY_PORT}":
    tls: { insecure_skip_verify: true }
  "${REGISTRY_PULL_HOST}":
    tls: { insecure_skip_verify: true }
EOF
  vd_info "restarting k3s so containerd reloads the registry config"
  systemctl restart k3s
fi

# Also fix the kubeconfig location — older install-platform.sh wrote it to
# /root/.kube/config, but docker-compose mounts ${REPO_ROOT}/kube. Without
# this the control-plane container has no kubeconfig and the K8s reconciler
# silently doesn't start.
KUBE_DIR="$REPO_ROOT/kube"
if [[ -f /etc/rancher/k3s/k3s.yaml ]]; then
  vd_info "syncing kubeconfig into $KUBE_DIR/config (with insecure-skip-tls-verify)"
  mkdir -p "$KUBE_DIR"
  awk '
    /^[[:space:]]*certificate-authority-data:/ { next }
    /^[[:space:]]*server:[[:space:]]+https:\/\/127\.0\.0\.1:6443/ {
      print "    insecure-skip-tls-verify: true"
      print "    server: https://host.docker.internal:6443"
      next
    }
    { print }
  ' /etc/rancher/k3s/k3s.yaml > "$KUBE_DIR/config"
  chmod 644 "$KUBE_DIR/config"
  vd_ok "kubeconfig at $KUBE_DIR/config"
else
  vd_warn "/etc/rancher/k3s/k3s.yaml not found — is K3s installed?"
fi

echo
echo "Next:"
echo "  docker compose up -d --build control-plane build-service"
echo
echo "Don't forget the workers — each one needs to learn ${REGISTRY_PULL_HOST}:"
echo "  ssh root@<worker>"
echo "  sudo PLATFORM_IP=${PLATFORM_IP} \\"
echo "       K3S_URL=https://${PLATFORM_IP}:6443 \\"
echo "       K3S_TOKEN=\$(cat /var/lib/rancher/k3s/server/node-token) \\"
echo "       REGISTRY_PULL_HOST=${REGISTRY_PULL_HOST} \\"
echo "       /opt/it-iai/deploy/install-worker.sh"
