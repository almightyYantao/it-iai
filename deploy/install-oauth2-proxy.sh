#!/usr/bin/env bash
# install-oauth2-proxy.sh — deploy oauth2-proxy + Traefik ForwardAuth middlewares
# so every user-deployed app on the platform requires Keycloak login.
#
# Run on the PLATFORM node:
#     sudo /opt/it-iai/deploy/install-oauth2-proxy.sh
#
# Reads runtime config from the database (system_config table) when it can, and
# falls back to the .env file otherwise. The script is idempotent — safe to
# re-run after changing the Keycloak client secret or auth host in the Web UI.
#
# What it installs:
#   1. Namespace `oauth2-proxy`
#   2. Secret with client_secret + cookie_secret
#   3. Deployment + Service running oauth2-proxy in proxy-only mode
#   4. Ingress for `auth.<base-domain>` so the OIDC redirect lands somewhere
#   5. Two Traefik Middlewares the control plane attaches to user ingresses:
#        - oauth2-proxy-forward-auth   (calls /oauth2/auth — 200 = pass, 401 = fail)
#        - oauth2-proxy-errors         (catches 401, redirects to /oauth2/start)
#
# After this, the control plane will attach those middlewares to every
# non-public project's ingress automatically.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
ENV_FILE="$REPO_ROOT/.env"

# --- styling ---------------------------------------------------------------

if [[ -t 1 ]]; then
  C_OK=$'\033[32m'; C_INFO=$'\033[36m'; C_WARN=$'\033[33m'
  C_ERR=$'\033[31m'; C_DIM=$'\033[2m'; C_BOLD=$'\033[1m'; C_OFF=$'\033[0m'
else
  C_OK="" C_INFO="" C_WARN="" C_ERR="" C_DIM="" C_BOLD="" C_OFF=""
fi
info()  { printf '%s•%s %s\n' "$C_INFO" "$C_OFF" "$*"; }
ok()    { printf '%s✓%s %s\n' "$C_OK"   "$C_OFF" "$*"; }
warn()  { printf '%s!%s %s\n' "$C_WARN" "$C_OFF" "$*"; }
die()   { printf '%s✗%s %s\n' "$C_ERR"  "$C_OFF" "$*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "must run as root (or via sudo)"

# --- locate kubectl --------------------------------------------------------
#
# k3s installs its binaries to /usr/local/bin, which is missing from sudo's
# secure_path on RHEL-family hosts. `command -v k3s` then fails even though
# the binary exists. Search the canonical install locations explicitly before
# giving up.

find_kubectl() {
  local candidates=(
    "k3s"
    "/usr/local/bin/k3s"
    "/usr/bin/k3s"
    "/opt/k3s/bin/k3s"
  )
  for c in "${candidates[@]}"; do
    if [[ "$c" == */* ]]; then
      [[ -x "$c" ]] || continue
    else
      command -v "$c" >/dev/null 2>&1 || continue
    fi
    KUBECTL=("$c" kubectl)
    return 0
  done
  # Standalone kubectl. Needs a kubeconfig; k3s drops one here on install.
  for c in kubectl /usr/local/bin/kubectl /usr/bin/kubectl; do
    if [[ "$c" == */* ]]; then
      [[ -x "$c" ]] || continue
    else
      command -v "$c" >/dev/null 2>&1 || continue
    fi
    KUBECTL=("$c")
    if [[ -z "${KUBECONFIG:-}" && -r /etc/rancher/k3s/k3s.yaml ]]; then
      export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
    fi
    return 0
  done
  return 1
}

find_kubectl || die "no working kubectl found (searched k3s + kubectl in PATH and /usr/local/bin, /usr/bin)"
"${KUBECTL[@]}" version --client >/dev/null 2>&1 || die "${KUBECTL[*]} exists but won't run: $(${KUBECTL[*]} version --client 2>&1 | head -n2)"

# --- gather config ---------------------------------------------------------
#
# Preference order, per knob:
#   1. environment variable already set when invoking this script
#   2. the system_config row in postgres (Admin Settings page wrote it)
#   3. the .env file (operator typed it on first install)
#   4. derived default (mainly for cookie_domain / auth_host)
#
# This means an admin who flipped a value in the Web UI doesn't have to also
# remember to edit .env before re-running this script.

# Pull from DB if the control-plane stack is up. Best-effort.
db_get() {
  local key="$1"
  docker compose -f "$REPO_ROOT/docker-compose.yml" exec -T postgres \
    psql -At -U vibedeploy -d vibedeploy \
    -c "SELECT value FROM system_config WHERE key = '${key//\'/}'" 2>/dev/null || true
}

# Pull from .env. Lines look like KEY=value (no spaces around =).
env_get() {
  local key="$1"
  [[ -f "$ENV_FILE" ]] || { echo ""; return 0; }
  awk -F= -v k="$key" '$1 == k { sub(/^[^=]*=/, ""); print; exit }' "$ENV_FILE"
}

# Resolve a value: env-override → DB row → .env → default.
resolve() {
  local override="${!1:-}"   # env var with the same name as $1
  local db_key="$2"
  local env_key="$3"
  local default="${4:-}"
  if [[ -n "$override" ]]; then echo "$override"; return; fi
  local v
  v=$(db_get "$db_key" 2>/dev/null || true)
  [[ -n "$v" ]] && { echo "$v"; return; }
  v=$(env_get "$env_key" 2>/dev/null || true)
  [[ -n "$v" ]] && { echo "$v"; return; }
  echo "$default"
}

BASE_DOMAIN=$(resolve BASE_DOMAIN ""                "CP_APP_BASE_DOMAIN" "")
[[ -z "$BASE_DOMAIN" ]] && die "BASE_DOMAIN unset (no CP_APP_BASE_DOMAIN in .env, no override). Set BASE_DOMAIN=... and retry."

KC_ISSUER=$(resolve  KC_ISSUER       "kc.issuer"            "CP_KC_ISSUER"        "")
KC_CLIENT_ID=$(resolve KC_CLIENT_ID  "kc.client_id"         "CP_KC_CLIENT_ID"     "")
KC_CLIENT_SECRET=$(resolve KC_CLIENT_SECRET "kc.client_secret" "CP_KC_CLIENT_SECRET" "")
AUTH_HOST=$(resolve  AUTH_HOST       "auth.host"            ""                    "auth.${BASE_DOMAIN}")
COOKIE_SECRET=$(resolve COOKIE_SECRET "auth.cookie_secret"  ""                    "")
COOKIE_DOMAIN=$(resolve COOKIE_DOMAIN "auth.cookie_domain"  ""                    ".${BASE_DOMAIN}")

for k in KC_ISSUER KC_CLIENT_ID KC_CLIENT_SECRET; do
  if [[ -z "${!k}" ]]; then
    die "$k is empty. Set it via the Admin Settings page (or .env), then re-run this script."
  fi
done

# oauth2-proxy uses the cookie secret directly as an AES key, so the STRING
# length (not the decoded byte length) must be 16, 24, or 32. `openssl rand
# -base64 32` would produce a 44-char string and crash startup. Pull 24
# random bytes, base64-encode (32 chars), strip padding, swap to url-safe
# alphabet — gives us a stable 32-character secret.
gen_cookie_secret() {
  head -c 24 /dev/urandom | base64 | tr -d '=\n' | tr '+/' '-_'
}

# If a previously-stored secret is the wrong length (older versions of this
# script generated 44-char strings), regenerate it. We detect that here
# rather than letting the deployment crashloop.
if [[ -n "$COOKIE_SECRET" ]]; then
  case ${#COOKIE_SECRET} in
    16|24|32) ;;
    *)
      warn "stored cookie_secret has length ${#COOKIE_SECRET}; oauth2-proxy needs 16/24/32. Regenerating."
      COOKIE_SECRET=""
      ;;
  esac
fi

# Auto-generate the cookie secret on first install — admin can override later
# via the Settings page and re-run. Stored back into the DB so the value is
# preserved across re-installs.
if [[ -z "$COOKIE_SECRET" ]]; then
  COOKIE_SECRET=$(gen_cookie_secret)
  info "generated cookie_secret (${#COOKIE_SECRET} chars; will be persisted to system_config)"
  docker compose -f "$REPO_ROOT/docker-compose.yml" exec -T postgres \
    psql -U vibedeploy -d vibedeploy -c "
      INSERT INTO system_config (key, value) VALUES ('auth.cookie_secret', '${COOKIE_SECRET//\'/\'\'}')
      ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();
      INSERT INTO system_config (key, value) VALUES ('auth.host', '${AUTH_HOST}')
      ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();
      INSERT INTO system_config (key, value) VALUES ('auth.cookie_domain', '${COOKIE_DOMAIN}')
      ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();
    " >/dev/null || warn "couldn't persist generated secret to DB (control plane down?) — keep a copy: $COOKIE_SECRET"
fi

REDIRECT_URL="https://${AUTH_HOST}/oauth2/callback"

cat <<EOF
${C_BOLD}Installing oauth2-proxy${C_OFF}
  ${C_DIM}base domain   :${C_OFF} ${BASE_DOMAIN}
  ${C_DIM}auth host     :${C_OFF} ${AUTH_HOST}
  ${C_DIM}cookie domain :${C_OFF} ${COOKIE_DOMAIN}
  ${C_DIM}issuer        :${C_OFF} ${KC_ISSUER}
  ${C_DIM}client id     :${C_OFF} ${KC_CLIENT_ID}
  ${C_DIM}redirect url  :${C_OFF} ${REDIRECT_URL}
EOF

# --- assemble manifest -----------------------------------------------------
#
# All resources go into a single namespace so an uninstall is a one-liner.

# Base64-encode secrets ourselves rather than relying on a kubectl --from-literal
# round-trip; gives us a single deterministic manifest to apply.
b64() { printf '%s' "$1" | base64 | tr -d '\n'; }

# Detect cert-manager. When a ClusterIssuer is present (installed via
# deploy/install-cert-manager.sh) we annotate the auth ingress so cert-manager
# issues a per-host LE cert via HTTP-01, removing the dependence on the
# wildcard TLSStore from install-tls.sh. Set CLUSTER_ISSUER=none to force the
# wildcard path even when cert-manager is present.
CLUSTER_ISSUER="${CLUSTER_ISSUER:-}"
if [[ -z "$CLUSTER_ISSUER" ]]; then
  for issuer in letsencrypt-prod letsencrypt-staging; do
    if "${KUBECTL[@]}" get clusterissuer "$issuer" >/dev/null 2>&1; then
      CLUSTER_ISSUER="$issuer"
      break
    fi
  done
fi
CERT_LINE=""
TLS_BLOCK=""
if [[ -n "$CLUSTER_ISSUER" && "$CLUSTER_ISSUER" != "none" ]]; then
  info "cert-manager detected — auth ingress will use ClusterIssuer $CLUSTER_ISSUER"
  CERT_LINE="    cert-manager.io/cluster-issuer: \"$CLUSTER_ISSUER\""
  TLS_BLOCK="  tls:
    - hosts: [\"$AUTH_HOST\"]
      secretName: oauth2-proxy-tls"
fi

MANIFEST=$(mktemp)
cat > "$MANIFEST" <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: oauth2-proxy
  labels:
    vibedeploy.io/managed: "true"
---
apiVersion: v1
kind: Secret
metadata:
  name: oauth2-proxy
  namespace: oauth2-proxy
type: Opaque
data:
  client-id:     $(b64 "$KC_CLIENT_ID")
  client-secret: $(b64 "$KC_CLIENT_SECRET")
  cookie-secret: $(b64 "$COOKIE_SECRET")
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: oauth2-proxy
  namespace: oauth2-proxy
  labels:
    app: oauth2-proxy
    vibedeploy.io/managed: "true"
spec:
  replicas: 1
  selector:
    matchLabels: { app: oauth2-proxy }
  template:
    metadata:
      labels: { app: oauth2-proxy }
    spec:
      containers:
        - name: oauth2-proxy
          image: quay.io/oauth2-proxy/oauth2-proxy:v7.6.0
          args:
            - --provider=keycloak-oidc
            - --oidc-issuer-url=${KC_ISSUER}
            - --redirect-url=${REDIRECT_URL}
            - --upstream=static://200
            - --http-address=0.0.0.0:4180
            - --reverse-proxy=true
            - --pass-access-token=true
            - --pass-authorization-header=true
            - --pass-user-headers=true
            - --set-xauthrequest=true
            - --set-authorization-header=true
            - --email-domain=*
            - --skip-provider-button=false
            - --cookie-secure=true
            - --cookie-httponly=true
            - --cookie-samesite=lax
            - --cookie-domain=${COOKIE_DOMAIN}
            - --whitelist-domain=${COOKIE_DOMAIN}
            - --silence-ping-logging=true
            - --request-logging=false
          env:
            - { name: OAUTH2_PROXY_CLIENT_ID,     valueFrom: { secretKeyRef: { name: oauth2-proxy, key: client-id     } } }
            - { name: OAUTH2_PROXY_CLIENT_SECRET, valueFrom: { secretKeyRef: { name: oauth2-proxy, key: client-secret } } }
            - { name: OAUTH2_PROXY_COOKIE_SECRET, valueFrom: { secretKeyRef: { name: oauth2-proxy, key: cookie-secret } } }
          ports:
            - { name: http, containerPort: 4180 }
          readinessProbe:
            httpGet: { path: /ping, port: http }
            initialDelaySeconds: 3
            periodSeconds: 5
          livenessProbe:
            httpGet: { path: /ping, port: http }
            initialDelaySeconds: 10
            periodSeconds: 30
          resources:
            requests: { cpu: "50m", memory: "64Mi" }
            limits:   { cpu: "500m", memory: "256Mi" }
---
apiVersion: v1
kind: Service
metadata:
  name: oauth2-proxy
  namespace: oauth2-proxy
spec:
  selector: { app: oauth2-proxy }
  ports:
    - { name: http, port: 80, targetPort: 4180 }
---
# Public-facing ingress at auth.<base-domain> — the OIDC redirect lands here,
# /oauth2/start kicks off the dance, /oauth2/callback finishes it.
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: oauth2-proxy
  namespace: oauth2-proxy
  annotations:
    traefik.ingress.kubernetes.io/router.entrypoints: web,websecure
${CERT_LINE}
spec:
  rules:
    - host: ${AUTH_HOST}
      http:
        paths:
          - path: /oauth2
            pathType: Prefix
            backend:
              service:
                name: oauth2-proxy
                port: { number: 80 }
${TLS_BLOCK}
---
# Middleware #1 — ForwardAuth. Attached to every protected user ingress.
# Traefik calls /oauth2/auth; 200 means the user has a valid session and
# Traefik proxies the request through. 401 means no session.
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: forward-auth
  namespace: oauth2-proxy
spec:
  forwardAuth:
    address: http://oauth2-proxy.oauth2-proxy.svc.cluster.local/oauth2/auth
    trustForwardHeader: true
    authResponseHeaders:
      - X-Auth-Request-User
      - X-Auth-Request-Email
      - X-Auth-Request-Preferred-Username
      - Authorization
---
# Middleware #2 — Errors. Catches the 401 from ForwardAuth and rewrites the
# response into a redirect to /oauth2/start, so unauthenticated users get
# bounced through Keycloak instead of seeing a raw 401.
#
# statusRewrites (Traefik v3+) is critical: without it the middleware
# preserves the original 401 status code and just swaps the body/headers,
# so the browser sees `401 + Location: ...` and refuses to follow the
# redirect — instead it renders oauth2-proxy's 302 HTML body verbatim
# ("<a href=...>Found</a>"). Rewriting to 302 makes the browser auto-follow.
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: errors-redirect
  namespace: oauth2-proxy
spec:
  errors:
    status:
      - "401"
    service:
      name: oauth2-proxy
      port: 80
    query: "/oauth2/start?rd={url}"
    statusRewrites:
      "401": 302
YAML

# --- apply -----------------------------------------------------------------

info "applying manifest"
"${KUBECTL[@]}" apply -f "$MANIFEST"
rm -f "$MANIFEST"

info "waiting for oauth2-proxy to become ready (60s timeout)"
if "${KUBECTL[@]}" -n oauth2-proxy rollout status deploy/oauth2-proxy --timeout=60s; then
  ok "oauth2-proxy rolled out"
else
  warn "rollout didn't complete — check: ${KUBECTL[*]} -n oauth2-proxy describe deploy oauth2-proxy"
fi

# --- summary ---------------------------------------------------------------

cat <<EOF

${C_BOLD}Done.${C_OFF} Next:

  1. Point DNS for ${C_BOLD}${AUTH_HOST}${C_OFF} at the platform IP (same A record as
     *.${BASE_DOMAIN}). TLS is handled by Traefik's default cert resolver.

  2. In Keycloak's client ${C_BOLD}${KC_CLIENT_ID}${C_OFF}, add this to "Valid redirect URIs":
       ${REDIRECT_URL}

  3. The control plane will now attach the ForwardAuth middlewares to every
     non-public project ingress on the next deploy or settings save. Existing
     deployments: trigger a re-deploy, or call:
       curl -X PATCH .../v1/projects/<slug>/access -d '{"allow_cidrs":[]}'
     (the access endpoint also re-syncs the ingress).

Test by hitting any project URL incognito — you should be bounced through Keycloak.
EOF
