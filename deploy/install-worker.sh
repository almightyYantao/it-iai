#!/usr/bin/env bash
# Install an iai K3s worker node and join it to the platform cluster.
# Idempotent: safe to re-run.
#
# Usage (run from the repo root, after install-platform.sh on the platform node):
#   sudo K3S_URL=https://10.0.0.5:6443 \
#        K3S_TOKEN=K10abcd... \
#        PLATFORM_IP=10.0.0.5 \
#        deploy/install-worker.sh
#
# Required env:
#   K3S_URL       https://<platform-ip>:6443 (from install-platform output)
#   K3S_TOKEN     node-token printed by install-platform
#   PLATFORM_IP   private IP of the platform node — used for the registry mirror
#
# Optional:
#   K3S_VERSION         pin a specific K3s version (must match the server)
#   REGISTRY_PORT       default 5001
#   NODE_IP             override this node's advertised IP
#   REGISTRY_PULL_HOST  host:port that K8s image tags use; must match what
#                       install-platform.sh wrote into the cluster's .env
#                       (CP_REGISTRY_HOST_FROM_K3D). Default ${PLATFORM_IP}:${REGISTRY_PORT}.
#                       Override when workers are on a different network than
#                       PLATFORM_IP, e.g. when using a public IP:
#                         REGISTRY_PULL_HOST=<public-ip>:5001

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=lib/_common.sh
source "$HERE/lib/_common.sh"

vd_require_root
vd_detect_os

: "${K3S_URL:?must set K3S_URL=https://<platform-ip>:6443}"
: "${K3S_TOKEN:?must set K3S_TOKEN=<from platform install output>}"
: "${PLATFORM_IP:?must set PLATFORM_IP=<platform node private ip>}"

NODE_IP="${NODE_IP:-$(vd_detect_ip)}"
REGISTRY_PORT="${REGISTRY_PORT:-5001}"
K3S_VERSION="${K3S_VERSION:-}"
REGISTRY_PULL_HOST="${REGISTRY_PULL_HOST:-${PLATFORM_IP}:${REGISTRY_PORT}}"

# Where the image-host name actually resolves: same machine as the registry,
# which is the platform node. We always route to PLATFORM_IP. REGISTRY_PULL_HOST
# is just the alias workers see in image tags.
REGISTRY_ENDPOINT_URL="http://${PLATFORM_IP}:${REGISTRY_PORT}"

vd_info "joining K3s server:  $K3S_URL"
vd_info "platform IP:         $PLATFORM_IP"
vd_info "this node IP:        $NODE_IP"
vd_info "registry pull host:  $REGISTRY_PULL_HOST  (will fetch from $REGISTRY_ENDPOINT_URL)"

# 1. Base packages -----------------------------------------------------------
case "$VD_OS_FAMILY" in
  debian) vd_pkg_install curl ca-certificates iproute2 ;;
  rhel)   vd_pkg_install curl ca-certificates iproute  ;;
esac

# 2. Pre-stage registries.yaml so the agent picks it up on first start ------
#
# Important: k3s/containerd ONLY rereads /etc/rancher/k3s/registries.yaml when
# the k3s-agent service starts. Writing a new file while the agent is already
# running has no effect until restart. We compare the new content against the
# existing file and force a restart below if it changed.
vd_info "configuring K3s registry mirrors → $REGISTRY_ENDPOINT_URL"
mkdir -p /etc/rancher/k3s
REG_FILE=/etc/rancher/k3s/registries.yaml
REG_TMP=$(mktemp)
# Mirror every image-host alias that could appear in a deployed image tag
# (legacy host.docker.internal, the platform's private IP, and the configured
# REGISTRY_PULL_HOST — typically a public IP/domain). All route to the same
# physical registry on the platform node.
cat > "$REG_TMP" <<EOF
mirrors:
  "host.docker.internal:${REGISTRY_PORT}":
    endpoint:
      - "${REGISTRY_ENDPOINT_URL}"
  "${PLATFORM_IP}:${REGISTRY_PORT}":
    endpoint:
      - "${REGISTRY_ENDPOINT_URL}"
  "${REGISTRY_PULL_HOST}":
    endpoint:
      - "${REGISTRY_ENDPOINT_URL}"
configs:
  "host.docker.internal:${REGISTRY_PORT}":
    tls: { insecure_skip_verify: true }
  "${PLATFORM_IP}:${REGISTRY_PORT}":
    tls: { insecure_skip_verify: true }
  "${REGISTRY_PULL_HOST}":
    tls: { insecure_skip_verify: true }
EOF

REG_CHANGED=false
if [[ ! -f "$REG_FILE" ]] || ! cmp -s "$REG_TMP" "$REG_FILE"; then
  mv "$REG_TMP" "$REG_FILE"
  REG_CHANGED=true
  vd_ok "wrote $REG_FILE"
else
  rm -f "$REG_TMP"
  vd_ok "$REG_FILE already up to date"
fi

# 3. Install / refresh K3s agent --------------------------------------------
if systemctl is-active --quiet k3s-agent 2>/dev/null; then
  if $REG_CHANGED; then
    vd_info "registry config changed — restarting k3s-agent so containerd reloads"
    systemctl restart k3s-agent
    vd_ok "k3s-agent restarted"
  else
    vd_ok "k3s-agent already running"
  fi
else
  vd_info "installing K3s agent"
  install_exec="agent --node-ip=${NODE_IP}"
  if [[ -n "$K3S_VERSION" ]]; then
    curl -sfL https://get.k3s.io | \
      INSTALL_K3S_VERSION="$K3S_VERSION" \
      K3S_URL="$K3S_URL" K3S_TOKEN="$K3S_TOKEN" \
      INSTALL_K3S_EXEC="$install_exec" sh -
  else
    curl -sfL https://get.k3s.io | \
      K3S_URL="$K3S_URL" K3S_TOKEN="$K3S_TOKEN" \
      INSTALL_K3S_EXEC="$install_exec" sh -
  fi
  vd_ok "K3s agent installed"
fi

# 4. Wait for the agent to register -----------------------------------------
vd_info "waiting up to 90s for the agent to register"
ok=false
for _ in $(seq 1 45); do
  if systemctl is-active --quiet k3s-agent 2>/dev/null; then
    ok=true; break
  fi
  sleep 2
done
$ok || { vd_err "k3s-agent failed to start"; journalctl -u k3s-agent --no-pager -n 50; exit 1; }

cat <<EOF

================================================================================
 K3s worker joined.
================================================================================

  Server: $K3S_URL
  Node IP: $NODE_IP

  Verify on the platform node:
    sudo k3s kubectl get nodes -o wide

  This worker can pull images from the platform registry over HTTP.
  Make sure the security group allows: ${NODE_IP} <-> ${PLATFORM_IP} on
    6443/tcp (kube api), 10250/tcp (kubelet), 8472/udp (flannel vxlan),
    51820/udp (wireguard, if enabled), ${REGISTRY_PORT}/tcp (registry).
================================================================================
EOF
