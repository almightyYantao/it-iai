#!/usr/bin/env bash
# install-admin-ui-tls.sh — expose the admin UI (docker-compose's web/nginx)
# through Traefik on a wildcard-covered subdomain so it gets HTTPS via the
# default TLSStore set up by install-tls.sh.
#
# Run on the PLATFORM node, AFTER install-tls.sh has been run:
#     sudo /opt/it-iai/deploy/install-admin-ui-tls.sh
#
# What it does:
#   1. Resolves the platform node's IP (the docker-compose nginx listens on
#      127.0.0.1:5173 there). Workers reach it via PLATFORM_IP:5173.
#   2. Creates a selectorless Service + Endpoints in kube-system that points
#      at PLATFORM_IP:5173 — a thin bridge from "inside the cluster" to the
#      docker-compose container.
#   3. Creates an Ingress for ADMIN_HOST (default admin.<BASE_DOMAIN>)
#      routing to that service. Traefik picks up the wildcard cert
#      automatically via the TLSStore set up by install-tls.sh.
#
# Idempotent.

set -uo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"

# --- styling ---------------------------------------------------------------

if [[ -t 1 ]]; then
  C_OK=$'\033[32m'; C_INFO=$'\033[36m'; C_WARN=$'\033[33m'
  C_ERR=$'\033[31m'; C_DIM=$'\033[2m'; C_BOLD=$'\033[1m'; C_OFF=$'\033[0m'
else
  C_OK="" C_INFO="" C_WARN="" C_ERR="" C_DIM="" C_BOLD="" C_OFF=""
fi
info() { printf '%s•%s %s\n' "$C_INFO" "$C_OFF" "$*"; }
ok()   { printf '%s✓%s %s\n' "$C_OK"   "$C_OFF" "$*"; }
warn() { printf '%s!%s %s\n' "$C_WARN" "$C_OFF" "$*"; }
die()  { printf '%s✗%s %s\n' "$C_ERR"  "$C_OFF" "$*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "must run as root (or via sudo)"

# --- locate kubectl --------------------------------------------------------

KUBECTL=()
for c in k3s /usr/local/bin/k3s /usr/bin/k3s; do
  if [[ "$c" == */* ]]; then [[ -x "$c" ]] || continue
  else command -v "$c" >/dev/null 2>&1 || continue
  fi
  KUBECTL=("$c" kubectl); break
done
if [[ ${#KUBECTL[@]} -eq 0 ]]; then
  for c in kubectl /usr/local/bin/kubectl /usr/bin/kubectl; do
    if [[ "$c" == */* ]]; then [[ -x "$c" ]] || continue
    else command -v "$c" >/dev/null 2>&1 || continue
    fi
    KUBECTL=("$c")
    [[ -z "${KUBECONFIG:-}" && -r /etc/rancher/k3s/k3s.yaml ]] && export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
    break
  done
fi
[[ ${#KUBECTL[@]} -gt 0 ]] || die "no working kubectl"

# --- resolve config --------------------------------------------------------

ENV_FILE="$REPO_ROOT/.env"
BASE_DOMAIN="${BASE_DOMAIN:-$(awk -F= '$1=="CP_APP_BASE_DOMAIN"{sub(/^[^=]*=/, ""); print; exit}' "$ENV_FILE" 2>/dev/null || true)}"
[[ -z "$BASE_DOMAIN" ]] && die "BASE_DOMAIN unset (no CP_APP_BASE_DOMAIN in .env). Set BASE_DOMAIN=... and retry."

ADMIN_HOST="${ADMIN_HOST:-admin.${BASE_DOMAIN}}"
WEB_PORT="${WEB_PORT:-5173}"

# Detect platform IP: prefer the private RFC1918 address on the default route
# (works on cloud ECS where the public IP is NAT'd onto a private interface).
# Falls back to whatever `ip route` says is the primary.
PLATFORM_IP="${PLATFORM_IP:-}"
if [[ -z "$PLATFORM_IP" ]]; then
  PLATFORM_IP=$(ip -4 -o route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}')
fi
if [[ -z "$PLATFORM_IP" ]]; then
  PLATFORM_IP=$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -n1)
fi
[[ -z "$PLATFORM_IP" ]] && die "couldn't auto-detect platform IP. Set PLATFORM_IP=... and retry."

# --- sanity: nginx is actually listening on that port locally --------------

if ! curl -fsS -m 3 -o /dev/null "http://127.0.0.1:${WEB_PORT}/"; then
  warn "127.0.0.1:${WEB_PORT} did not respond — is docker compose up? continuing anyway"
else
  ok "admin UI reachable at 127.0.0.1:${WEB_PORT}"
fi

cat <<EOF

${C_BOLD}Bridging the admin UI through Traefik${C_OFF}
  ${C_DIM}admin host    :${C_OFF} $ADMIN_HOST
  ${C_DIM}upstream      :${C_OFF} ${PLATFORM_IP}:${WEB_PORT} (docker-compose nginx on platform node)
  ${C_DIM}wildcard cert :${C_OFF} via TLSStore/default (install-tls.sh)
EOF

# --- apply -----------------------------------------------------------------

info "applying Service/Endpoints/Ingress for $ADMIN_HOST"
cat <<YAML | "${KUBECTL[@]}" apply -f - >/dev/null
---
# Selectorless Service + Endpoints pattern — a Service with no selector
# doesn't auto-discover pods, so the Endpoints we apply by name take over.
# That lets us route cluster traffic to an IP/port that lives OUTSIDE the
# cluster (the docker-compose nginx on the platform node).
apiVersion: v1
kind: Service
metadata:
  name: admin-ui
  namespace: kube-system
  labels: { vibedeploy.io/managed: "true" }
spec:
  ports:
    - name: http
      port: 80
      targetPort: ${WEB_PORT}
      protocol: TCP
---
apiVersion: v1
kind: Endpoints
metadata:
  name: admin-ui            # name MUST match the Service for the binding
  namespace: kube-system
  labels: { vibedeploy.io/managed: "true" }
subsets:
  - addresses:
      - ip: ${PLATFORM_IP}
    ports:
      - name: http
        port: ${WEB_PORT}
        protocol: TCP
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: admin-ui
  namespace: kube-system
  labels: { vibedeploy.io/managed: "true" }
  annotations:
    # Both entrypoints — Traefik's HelmChartConfig redirects web→websecure,
    # so this just gives the redirect something to match on for HTTP.
    traefik.ingress.kubernetes.io/router.entrypoints: web,websecure
spec:
  rules:
    - host: ${ADMIN_HOST}
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: admin-ui
                port: { number: 80 }
YAML
ok "Service / Endpoints / Ingress applied"

# --- summary ---------------------------------------------------------------

cat <<EOF

${C_BOLD}Done.${C_OFF}

  1. Point DNS for ${C_BOLD}${ADMIN_HOST}${C_OFF} → platform public IP.
     (Same record style as *.${BASE_DOMAIN}.)

  2. Hit it: ${C_BOLD}https://${ADMIN_HOST}/${C_OFF}
     Traefik serves the wildcard cert; the request lands on
     ${PLATFORM_IP}:${WEB_PORT} (the docker-compose nginx).

  3. Once you've confirmed it works, the old http://<ip>:${WEB_PORT}/ entry
     is no longer needed. You can keep the port open for emergency access
     or drop it from docker-compose.yml's ports: section.

${C_BOLD}If it doesn't work${C_OFF}
  - Check ingress accepted:   ${KUBECTL[*]} -n kube-system describe ingress admin-ui
  - Check the bridge service: ${KUBECTL[*]} -n kube-system get svc,endpoints admin-ui
  - From the platform node:   curl -kI https://${ADMIN_HOST}/
  - Traefik logs:             ${KUBECTL[*]} -n kube-system logs -l app.kubernetes.io/name=traefik --tail=80
EOF
