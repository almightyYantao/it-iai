#!/usr/bin/env bash
# Bootstrap a local k3d cluster wired to the docker-compose stack.
# Idempotent: safe to re-run.

set -euo pipefail
CLUSTER="${CLUSTER:-vibedeploy}"
BASE_DOMAIN="${BASE_DOMAIN:-lab.localhost}"
HERE="$(cd "$(dirname "$0")" && pwd)"

need() { command -v "$1" >/dev/null || { echo "missing: $1"; exit 1; }; }
need k3d
need kubectl
need docker

if ! k3d cluster list | awk 'NR>1 {print $1}' | grep -qx "$CLUSTER"; then
  echo ">> creating k3d cluster $CLUSTER"
  k3d cluster create "$CLUSTER" \
    --servers 1 --agents 2 \
    --port "80:80@loadbalancer" \
    --port "443:443@loadbalancer" \
    --registry-config "$HERE/registries.yaml" \
    --k3s-arg "--disable=servicelb@server:*"
else
  echo ">> k3d cluster $CLUSTER already exists"
fi

# Make sure host.k3d.internal resolves inside the cluster.
echo ">> sanity-checking host.k3d.internal -> $(k3d node list | awk '/server/ {print $1}' | head -1)"

# Wait for Traefik (installed by k3d by default).
echo ">> waiting for traefik to be ready"
kubectl -n kube-system rollout status deploy/traefik --timeout=120s || true

cat <<EOF

K3d cluster ready.
- Base domain: $BASE_DOMAIN
- LoadBalancer ports: 80, 443 mapped to host.
- DNS: add to /etc/hosts (M1 dev) for each project you push, e.g.:
    127.0.0.1   sales-dashboard-7k2x.$BASE_DOMAIN
  Or use dnsmasq / *.localhost wildcard:
    address=/$BASE_DOMAIN/127.0.0.1

Next:
  export KUBECONFIG=\$(k3d kubeconfig write $CLUSTER)
  docker compose up -d
  docker compose logs control-plane    # grab the bootstrap deploy token
EOF
