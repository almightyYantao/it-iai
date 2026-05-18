#!/usr/bin/env bash
# Install iai on a Linux platform node (K3s server + docker-compose stack).
# Idempotent: safe to re-run.
#
# Usage (run from the repo root):
#   sudo BASE_DOMAIN=lab.example.com deploy/install-platform.sh
#
# Environment overrides:
#   BASE_DOMAIN        Public hostname suffix for user apps (default: lab.<hostname>)
#   PLATFORM_IP        IP workers use to reach this node (default: auto-detect, usually
#                      the private subnet IP — that is correct for K3s)
#   EXTERNAL_API_HOST  Host:port clients use to reach the Control Plane API
#                      (default: ${PLATFORM_IP}:8080). Override when you have a public
#                      IP / domain that differs from PLATFORM_IP, e.g.
#                      EXTERNAL_API_HOST=<public-ip>:8080
#                      EXTERNAL_API_HOST=api.lab.example.com
#   EXTERNAL_API_TLS   "true" if EXTERNAL_API_HOST is behind https. Default: false.
#   K3S_VERSION        Pin a specific K3s version (default: latest stable)
#   REGISTRY_PORT      Host port for the dev registry (default: 5001)
#   REGISTRY_PULL_HOST Host:port that K3s nodes use to PULL images
#                      (default: ${PLATFORM_IP}:${REGISTRY_PORT}). Override
#                      when workers are on a different network from PLATFORM_IP,
#                      e.g. REGISTRY_PULL_HOST=<public-ip>:5001
#   S3_PUBLIC_HOST     Host:port clients use to reach MinIO (default: ${PLATFORM_IP}:9000)

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
# shellcheck source=lib/_common.sh
source "$HERE/lib/_common.sh"

vd_require_root
vd_detect_os

PLATFORM_IP="${PLATFORM_IP:-$(vd_detect_ip)}"
BASE_DOMAIN="${BASE_DOMAIN:-lab.$(hostname -s).local}"
REGISTRY_PORT="${REGISTRY_PORT:-5001}"
K3S_VERSION="${K3S_VERSION:-}"

EXTERNAL_API_HOST="${EXTERNAL_API_HOST:-${PLATFORM_IP}:8080}"
EXTERNAL_API_TLS="${EXTERNAL_API_TLS:-false}"
S3_PUBLIC_HOST="${S3_PUBLIC_HOST:-${PLATFORM_IP}:9000}"
REGISTRY_PULL_HOST="${REGISTRY_PULL_HOST:-${PLATFORM_IP}:${REGISTRY_PORT}}"
api_scheme="http"
[[ "$EXTERNAL_API_TLS" == "true" ]] && api_scheme="https"
EXTERNAL_BASE_URL="${api_scheme}://${EXTERNAL_API_HOST}"

[[ -n "$PLATFORM_IP" ]] || vd_die "could not detect platform IP; set PLATFORM_IP=..."

vd_info "platform IP:        $PLATFORM_IP"
vd_info "base domain:        $BASE_DOMAIN"
vd_info "registry (local):   127.0.0.1:${REGISTRY_PORT}    (build-service push)"
vd_info "registry (pull):    $REGISTRY_PULL_HOST       (K3s nodes pull)"
vd_info "external API URL:   $EXTERNAL_BASE_URL"
vd_info "external S3 host:   $S3_PUBLIC_HOST"
vd_info "OS:                 ${VD_OS_ID} ${VD_OS_VER}"

# Warn loudly if PLATFORM_IP is RFC1918 but the user didn't override EXTERNAL_API_HOST.
# Clients outside the private subnet won't be able to reach that IP.
if [[ -z "${EXTERNAL_API_HOST_OVERRIDE:-}" ]]; then
  if [[ "$PLATFORM_IP" =~ ^(10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[01])\.) ]]; then
    vd_warn "PLATFORM_IP $PLATFORM_IP is in an RFC1918 private range."
    vd_warn "  If your clients (Skill / Web UI) hit this node via a different"
    vd_warn "  public IP or domain, re-run with:"
    vd_warn "    EXTERNAL_API_HOST=<public-ip-or-domain>:8080 $0"
    vd_warn "  Otherwise events_url in deployment responses will point to the"
    vd_warn "  private IP and remote clients can't follow them."
  fi
fi

# 1. Base packages -----------------------------------------------------------
vd_info "installing base packages"
case "$VD_OS_FAMILY" in
  debian) vd_pkg_install curl ca-certificates jq tar zstd iproute2 ;;
  rhel)   vd_pkg_install curl ca-certificates jq tar zstd iproute  ;;
esac
vd_ok "base packages ready"

# 2. Docker ------------------------------------------------------------------
vd_install_docker

# 3. K3s server --------------------------------------------------------------
if ! systemctl is-active --quiet k3s 2>/dev/null; then
  vd_info "installing K3s server"
  k3s_url="https://get.k3s.io"
  # K3s's default Traefik is a LoadBalancer-type Service exposed via servicelb
  # (klipper-lb), which binds the node's 80/443 via a DaemonSet. We patch
  # Traefik to hostNetwork mode below — that does the same thing more directly
  # (Traefik pod binds 80/443 itself, no NAT hop). Either works; hostNetwork
  # is one fewer moving piece if servicelb misbehaves.
  install_exec="server --write-kubeconfig-mode=644 --node-ip=${PLATFORM_IP} --tls-san=${PLATFORM_IP}"
  if [[ -n "$K3S_VERSION" ]]; then
    curl -sfL "$k3s_url" | INSTALL_K3S_VERSION="$K3S_VERSION" INSTALL_K3S_EXEC="$install_exec" sh -
  else
    curl -sfL "$k3s_url" | INSTALL_K3S_EXEC="$install_exec" sh -
  fi
  vd_ok "K3s server installed"
else
  vd_ok "K3s already running"
fi

# Wait for the kubeconfig to materialise.
for _ in $(seq 1 30); do
  [[ -s /etc/rancher/k3s/k3s.yaml ]] && break
  sleep 1
done
[[ -s /etc/rancher/k3s/k3s.yaml ]] || vd_die "k3s.yaml not present after 30s"

# 4. K3s registry mirror (server) --------------------------------------------
# The platform's own containerd needs to know how to pull the image hostname
# that K8s deployments will be tagged with. We list every alias that could
# appear in an image tag — host.docker.internal (legacy), the platform's
# private IP, and the configured REGISTRY_PULL_HOST (could be a public IP /
# domain). All route to the local registry on the host.
vd_info "configuring K3s registry mirrors"
mkdir -p /etc/rancher/k3s
cat > /etc/rancher/k3s/registries.yaml <<EOF
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
systemctl restart k3s

# 4b. Configure Traefik to bind the host's :80 / :443 directly -----------------
#
# K3s's default Traefik is a LoadBalancer-type Service listening on
# container :8000 / :8443; without an external LB or servicelb the node's
# :80 / :443 stay unbound.
#
# Why HelmChartConfig instead of `kubectl patch deploy traefik`:
#   K3s deploys Traefik via the Helm chart. Any time K3s reconciles, helm
#   re-applies the chart and overwrites a patched Deployment. HelmChartConfig
#   tells the helm-controller to use *these* values when (re-)applying, so the
#   change survives restarts and K3s upgrades.
#
# Values:
#   hostNetwork=true                  pod binds the host's network namespace
#   ports.web.{port,exposedPort}=80   container listens on 80, service publishes 80
#   ports.websecure.{...}=443         same for HTTPS
vd_info "waiting for kube API after k3s restart"
for _ in $(seq 1 30); do
  if k3s kubectl get nodes >/dev/null 2>&1; then break; fi
  sleep 2
done

vd_info "applying HelmChartConfig to bind Traefik on host :80 / :443"
# Writing to /var/lib/rancher/k3s/server/manifests is the K3s-blessed way: the
# server watches this dir and applies anything in it idempotently.
mkdir -p /var/lib/rancher/k3s/server/manifests
cat > /var/lib/rancher/k3s/server/manifests/traefik-config.yaml <<'EOF'
apiVersion: helm.cattle.io/v1
kind: HelmChartConfig
metadata:
  name: traefik
  namespace: kube-system
spec:
  valuesContent: |-
    # DaemonSet — one Traefik per node so any node's :80 routes traffic,
    # regardless of which node a project pod lands on.
    deployment:
      kind: DaemonSet
      # hostNetwork=true makes the pod default to the host /etc/resolv.conf,
      # which on cloud VMs (e.g. Aliyun's 100.100.2.136) can't resolve
      # *.svc.cluster.local — any middleware referencing a cross-namespace
      # Service (oauth2-proxy, etc.) then fails with "no such host" and
      # Traefik returns 500. Force the pod to query CoreDNS first.
      dnsPolicy: ClusterFirstWithHostNet
    # When you put hostNetwork on a DaemonSet, the chart needs maxUnavailable>0
    # (or it can't roll a node — pod port is already taken). AND maxSurge must
    # be 0 (DaemonSet allows only one of them non-zero; the chart defaults
    # maxSurge=1 from its Deployment-shaped template).
    updateStrategy:
      type: RollingUpdate
      rollingUpdate:
        maxUnavailable: 1
        maxSurge: 0
    hostNetwork: true
    ports:
      web:
        port: 80
        exposedPort: 80
      websecure:
        port: 443
        exposedPort: 443
      # Move Traefik's internal admin port off :8080 — that's where the iai
      # control-plane API lives in docker-compose, and hostNetwork=true makes
      # this DaemonSet pod compete for the same host port on the platform node.
      traefik:
        port: 9099
        exposedPort: 9099
      metrics:
        port: 9101
        exposedPort: 9101
    # Traefik runs as user 65532 by default; non-root processes can't bind <1024
    # without CAP_NET_BIND_SERVICE. Run as root with the cap explicitly added.
    securityContext:
      capabilities:
        add: [NET_BIND_SERVICE]
        drop: [ALL]
      runAsUser: 0
      runAsNonRoot: false
EOF

# Wait for helm-controller to roll the new Traefik out.
vd_info "waiting for traefik pod to bind :80 (up to 3min)"
ok=false
for _ in $(seq 1 90); do
  if ss -tlnp 2>/dev/null | grep -qE ':80\b'; then
    ok=true; break
  fi
  sleep 2
done
if $ok; then
  vd_ok "traefik bound to host :80 / :443"
else
  vd_warn "traefik did not bind :80 within 3min — diagnose with:"
  vd_warn "  k3s kubectl -n kube-system get pods | grep traefik"
  vd_warn "  k3s kubectl -n kube-system logs helm-install-traefik"
fi

# 5. kubeconfig for compose (control-plane container reads /kube/config) ----
#
# docker-compose mounts ${REPO_ROOT}/kube:/kube:ro for the control-plane
# container. We write the rewritten kubeconfig there so the container can
# actually find it. Also drop a copy at /root/.kube/config so root-shell
# kubectl works.
vd_info "preparing kubeconfig for the control-plane container"
KUBE_DIR="$REPO_ROOT/kube"
mkdir -p "$KUBE_DIR" /root/.kube
# Two transforms on the K3s kubeconfig:
#   1. server URL: 127.0.0.1 -> host.docker.internal so containers can reach it.
#   2. drop certificate-authority-data + add insecure-skip-tls-verify=true.
#      K3s's self-signed server cert is for [hostname, kubernetes(.*), localhost],
#      NOT host.docker.internal — without skip-verify the API call fails on
#      hostname-mismatch. Same-host loopback, TLS hostname check is theatrical.
awk '
  /^[[:space:]]*certificate-authority-data:/ { next }
  /^[[:space:]]*server:[[:space:]]+https:\/\/127\.0\.0\.1:6443/ {
    print "    insecure-skip-tls-verify: true"
    print "    server: https://host.docker.internal:6443"
    next
  }
  { print }
' /etc/rancher/k3s/k3s.yaml > "$KUBE_DIR/config"
chmod 644 "$KUBE_DIR/config"   # 644 so the unprivileged container user can read it
cp -p "$KUBE_DIR/config" /root/.kube/config
chmod 600 /root/.kube/config
vd_ok "kubeconfig at $KUBE_DIR/config (mounted into container as /kube/config)"

# 6. .env --------------------------------------------------------------------
ENV_FILE="$REPO_ROOT/.env"
if [[ ! -f "$ENV_FILE" ]]; then
  vd_info "generating $ENV_FILE"
  KEK_B64=$(head -c 32 /dev/urandom | base64)
  # Generate a random admin password for user-postgres. 32 url-safe chars.
  USER_PG_ADMIN_PASSWORD=$(head -c 24 /dev/urandom | base64 | tr -d '=\n' | tr '+/' '-_')
  cat > "$ENV_FILE" <<EOF
# Generated by install-platform.sh on $(date -u +%FT%TZ)
CP_LISTEN_ADDR=:8080
CP_DATABASE_URL=postgres://vibedeploy:vibedeploy@postgres:5432/vibedeploy?sslmode=disable
CP_PUBLIC_BASE_URL=${EXTERNAL_BASE_URL}
CP_APP_BASE_DOMAIN=${BASE_DOMAIN}

CP_S3_ENDPOINT=minio:9000
CP_S3_REGION=us-east-1
CP_S3_ACCESS_KEY=vibedeploy
CP_S3_SECRET_KEY=vibedeploypw
CP_S3_BUCKET_SOURCE=vibedeploy-source
CP_S3_USE_SSL=false

# Public hostname baked into presigned URLs that the Skill consumes.
# Default to <platform-ip>:9000. Replace with a real domain (and set
# CP_S3_PUBLIC_USE_SSL=true) once you put nginx/TLS in front.
CP_S3_PUBLIC_ENDPOINT=${S3_PUBLIC_HOST}
CP_S3_PUBLIC_USE_SSL=false

# Two registry hostnames — push and pull are different audiences:
#
#   CP_REGISTRY_HOST  — what build-service tags + pushes with. push goes through
#     the host docker daemon (via mounted /var/run/docker.sock), so the daemon
#     — not the container — resolves this. Loopback wins:
#       * 127.0.0.1 is in docker daemon's default insecure-registries list,
#         so HTTP works with zero config.
#       * No DNS hops, no daemon json edits needed.
#
#   CP_REGISTRY_HOST_FROM_K3D — what we rewrite the image tag to before applying
#     to K8s. K3s/containerd on workers can't reach the platform's 127.0.0.1, so
#     we use the platform's private IP. Both the platform's and workers'
#     registries.yaml mark this host as insecure.
#
# k8sdriver.rewriteImageHost() swaps the registry hostname at apply time, so
# build-service can stay loopback-only while K3s sees the routable IP.
CP_REGISTRY_HOST=127.0.0.1:${REGISTRY_PORT}
CP_REGISTRY_HOST_FROM_K3D=${REGISTRY_PULL_HOST}

CP_KEK_BASE64=${KEK_B64}

# Keycloak — leave blank to fall back to Deploy Token-only.
# Fill in to enable JWT-bearer auth and the Web UI's "Sign in with Keycloak" button.
# Detailed shape: see .env.example in the repo.
CP_KC_ISSUER=
CP_KC_JWKS_URL=
# Audience check off by default — most KC setups don't have a vibedeploy-api
# mapper out of the box. See .env.example for details.
CP_KC_AUDIENCE=
CP_KC_AUTHORIZATION_URL=
CP_KC_TOKEN_URL=
CP_KC_CLIENT_ID=
CP_KC_CLIENT_SECRET=
CP_KC_REDIRECT_URL=

CP_KUBECONFIG=/kube/config
CP_K8S_INGRESS_CLASS=traefik

# ---- user-app sidecar auto-provisioning ----------------------------------
# When a project's manifest says \`needs.postgres = true\` (or \`needs.redis\`),
# control-plane auto-creates a database / URL and injects DATABASE_URL /
# REDIS_URL into the pod. Leave any of these blank to disable.
#
# user-postgres is a separate compose service from the platform PG above —
# isolation between user data and platform tables.
CP_USER_PG_ADMIN_USER=iaiadmin
CP_USER_PG_ADMIN_PASSWORD=${USER_PG_ADMIN_PASSWORD}
# Admin URL is what control-plane uses to run CREATE DATABASE etc. inside
# the compose network — hence the service-name hostname.
CP_USER_PG_ADMIN_URL=postgres://iaiadmin:${USER_PG_ADMIN_PASSWORD}@user-postgres:5432/postgres?sslmode=disable
# Public host is what the user pod sees in its DATABASE_URL. User pods live
# in K3s, not in compose's network, so they reach the platform host's
# published port 5433.
CP_USER_PG_PUBLIC_HOST=${PLATFORM_IP}:5433
# Shared Redis (the compose 'redis' service). Each project gets the same
# URL — namespace via key prefix in your app code.
CP_USER_REDIS_URL_TEMPLATE=redis://${PLATFORM_IP}:6379/0

BS_DATABASE_URL=\${CP_DATABASE_URL}
BS_S3_ENDPOINT=\${CP_S3_ENDPOINT}
BS_S3_ACCESS_KEY=\${CP_S3_ACCESS_KEY}
BS_S3_SECRET_KEY=\${CP_S3_SECRET_KEY}
BS_S3_BUCKET_SOURCE=\${CP_S3_BUCKET_SOURCE}
BS_S3_USE_SSL=false
BS_REGISTRY_HOST=\${CP_REGISTRY_HOST}
BS_WORK_DIR=/var/lib/vibedeploy/build
BS_POLL_INTERVAL=2s
BS_MAX_CONCURRENT=2
EOF
  chmod 600 "$ENV_FILE"
  vd_ok ".env generated (random KEK; do NOT commit)"
else
  vd_ok ".env already exists; leaving as-is"
fi

# 7. docker compose up -------------------------------------------------------
vd_info "bringing up docker-compose stack (this builds the Go binaries — first run takes 1-2 min)"
cd "$REPO_ROOT"
docker compose pull --ignore-buildable 2>/dev/null || true
docker compose up -d --build

# 8. Wait for control-plane health ------------------------------------------
vd_info "waiting for control-plane to be healthy"
if ! vd_wait_url "http://127.0.0.1:8080/healthz" 120; then
  vd_err "control-plane did not become healthy in 120s"
  docker compose logs --tail=80 control-plane
  exit 1
fi
vd_ok "control-plane is healthy"

# 9. Extract bootstrap token -------------------------------------------------
TOKEN=""
for _ in $(seq 1 30); do
  TOKEN=$(docker compose logs control-plane 2>/dev/null \
    | awk '/BOOTSTRAP DEPLOY TOKEN/{ getline; gsub(/^[[:space:]]+|[[:space:]]+$/,""); print; exit }')
  [[ -n "$TOKEN" ]] && break
  sleep 1
done

K3S_TOKEN="$(cat /var/lib/rancher/k3s/server/node-token 2>/dev/null || echo '<not-readable>')"

# 10. Done -------------------------------------------------------------------
cat <<EOF

================================================================================
 iai platform node is up.
================================================================================

  Control Plane API (clients):  ${EXTERNAL_BASE_URL}
  Control Plane API (private):  http://${PLATFORM_IP}:8080
  MinIO console:                http://${PLATFORM_IP}:9001        (vibedeploy / vibedeploypw)
  Image registry:                http://${PLATFORM_IP}:${REGISTRY_PORT}  (HTTP, no auth — keep on private net!)
  K3s API:                       https://${PLATFORM_IP}:6443

  App ingress (Traefik): http://*.${BASE_DOMAIN} via :80 on this host
  Point a wildcard A record  *.${BASE_DOMAIN} -> ${PLATFORM_IP}.

  Bootstrap Deploy Token (use as VIBEDEPLOY_TOKEN):
    ${TOKEN:-<failed to extract; see: docker compose logs control-plane>}

  Join workers — run on each worker node:

    sudo PLATFORM_IP=${PLATFORM_IP} \\
         K3S_URL=https://${PLATFORM_IP}:6443 \\
         K3S_TOKEN=${K3S_TOKEN} \\
         REGISTRY_PULL_HOST=${REGISTRY_PULL_HOST} \\
         deploy/install-worker.sh

  Skill setup (on a client laptop):
    export VIBEDEPLOY_API=${EXTERNAL_BASE_URL}
    export VIBEDEPLOY_TOKEN=${TOKEN:-<see above>}
    bash skill/scripts/push.sh

Security checklist before going beyond localhost:
  - Firewall: allow only the private subnet to reach :6443 / :5001 / :5432 / :9000.
  - Public ingress: open :80 (and :443 once you wire TLS in M3).
  - .env contains a freshly generated KEK — keep it 0600 and don't commit it.
================================================================================
EOF
