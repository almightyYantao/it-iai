#!/usr/bin/env bash
# install-cert-manager.sh — install cert-manager and create a
# Let's Encrypt HTTP-01 ClusterIssuer that the control-plane will
# attach to Ingresses for projects that have TLS turned on.
#
# Run on the PLATFORM node, after install-platform.sh has finished
# and Traefik is serving :80 / :443:
#
#     sudo ACME_EMAIL=you@example.com deploy/install-cert-manager.sh
#
# What it does:
#   1. Applies the upstream cert-manager manifest (CRDs + controllers).
#   2. Waits for the cert-manager Deployments to roll out.
#   3. Creates two ClusterIssuers — letsencrypt-prod (default) and
#      letsencrypt-staging (for safe testing against LE's higher-limit
#      staging endpoint). Both use HTTP-01 with Traefik as the solver,
#      so no DNS API integration is required.
#
# Per-app certs (not wildcards) are issued on demand: the control-plane
# annotates each Ingress with `cert-manager.io/cluster-issuer:
# letsencrypt-prod` when a project opts into HTTPS, and cert-manager
# solves the HTTP-01 challenge by spinning up a temporary Ingress
# rule pointing at its own challenge solver. Requires :80 reachable
# from the public internet for every app hostname.
#
# Wildcard certs are NOT supported via HTTP-01 by Let's Encrypt — those
# require DNS-01 with a DNS provider plugin. If you need a wildcard,
# keep using deploy/install-tls.sh with a hand-issued cert.
#
# Idempotent. Safe to re-run after changing CERT_MANAGER_VERSION or
# ACME_EMAIL.

set -uo pipefail

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
[[ ${#KUBECTL[@]} -gt 0 ]] || die "no working kubectl. Install k3s or kubectl."

# --- args / env ------------------------------------------------------------

CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.15.3}"
ACME_EMAIL="${ACME_EMAIL:-}"
ACME_SERVER_PROD="${ACME_SERVER_PROD:-https://acme-v02.api.letsencrypt.org/directory}"
ACME_SERVER_STAGING="${ACME_SERVER_STAGING:-https://acme-staging-v02.api.letsencrypt.org/directory}"
INGRESS_CLASS="${INGRESS_CLASS:-traefik}"

if [[ -z "$ACME_EMAIL" ]]; then
  warn "ACME_EMAIL is unset — Let's Encrypt requires a valid email for issuance."
  warn "Pass it via env: sudo ACME_EMAIL=you@example.com $0"
  die "refusing to apply ClusterIssuer without an email"
fi

cat <<EOF

${C_BOLD}Installing cert-manager${C_OFF}
  ${C_DIM}version       :${C_OFF} $CERT_MANAGER_VERSION
  ${C_DIM}ACME email    :${C_OFF} $ACME_EMAIL
  ${C_DIM}prod server   :${C_OFF} $ACME_SERVER_PROD
  ${C_DIM}staging server:${C_OFF} $ACME_SERVER_STAGING
  ${C_DIM}ingress class :${C_OFF} $INGRESS_CLASS

EOF

# --- 1. apply cert-manager manifest ----------------------------------------

# We use the official "static" manifest (CRDs + controllers in one file)
# rather than Helm so this script stays self-contained — no extra tooling
# required on the platform node.
info "applying cert-manager manifest ($CERT_MANAGER_VERSION)"
MANIFEST_URL="https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
if ! "${KUBECTL[@]}" apply -f "$MANIFEST_URL" >/dev/null; then
  die "failed to apply $MANIFEST_URL — check network egress / kube state"
fi
ok "cert-manager manifest applied"

# --- 2. wait for cert-manager to be ready ----------------------------------

info "waiting up to 3min for cert-manager Deployments to roll out"
deadline=$(( $(date +%s) + 180 ))
deps=(cert-manager cert-manager-cainjector cert-manager-webhook)
all_ready=false
while (( $(date +%s) < deadline )); do
  ready=0
  for d in "${deps[@]}"; do
    if "${KUBECTL[@]}" -n cert-manager rollout status deploy/"$d" --timeout=10s >/dev/null 2>&1; then
      ready=$((ready + 1))
    fi
  done
  if (( ready == ${#deps[@]} )); then
    all_ready=true
    break
  fi
  sleep 5
done
if $all_ready; then
  ok "cert-manager controllers ready"
else
  warn "cert-manager not fully ready after 3min — diagnose with:"
  warn "  ${KUBECTL[*]} -n cert-manager get pods"
  warn "  ${KUBECTL[*]} -n cert-manager describe deploy/cert-manager-webhook"
fi

# --- 3. create ClusterIssuers ----------------------------------------------

info "creating ClusterIssuer letsencrypt-prod + letsencrypt-staging"
cat <<YAML | "${KUBECTL[@]}" apply -f - >/dev/null
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    email: ${ACME_EMAIL}
    server: ${ACME_SERVER_PROD}
    privateKeySecretRef:
      # cert-manager stores the ACME account private key here. One per issuer
      # so prod and staging don't share an account.
      name: letsencrypt-prod-account-key
    solvers:
      - http01:
          ingress:
            class: ${INGRESS_CLASS}
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-staging
spec:
  acme:
    email: ${ACME_EMAIL}
    server: ${ACME_SERVER_STAGING}
    privateKeySecretRef:
      name: letsencrypt-staging-account-key
    solvers:
      - http01:
          ingress:
            class: ${INGRESS_CLASS}
YAML
ok "ClusterIssuers applied"

# --- 4. summary ------------------------------------------------------------

cat <<EOF

${C_BOLD}Done.${C_OFF}

  Default ClusterIssuer used by the control-plane:
    ${C_BOLD}letsencrypt-prod${C_OFF}

  Override per-deployment via env on the control-plane container:
    CP_TLS_CLUSTER_ISSUER=letsencrypt-staging

  ${C_BOLD}How a project gets HTTPS${C_OFF}
    1. Project owner toggles "Enable HTTPS" in Project Settings (or
       PATCH /v1/projects/<slug>/tls   { "enabled": true }).
    2. Control-plane rewrites the Ingress with a cert-manager annotation
       + tls: section listing every project hostname.
    3. cert-manager solves an HTTP-01 challenge per hostname (typically
       ~30s) and stores the cert in Secret iai-tls-<slug>.
    4. Traefik picks up the cert and serves the project over :443.

  ${C_BOLD}Verify after first issuance${C_OFF}
    ${KUBECTL[*]} -n proj-<slug> get certificate
    ${KUBECTL[*]} -n proj-<slug> describe certificate iai-tls-<slug>
    ${KUBECTL[*]} -n cert-manager logs deploy/cert-manager -f

  ${C_BOLD}Prerequisites for HTTP-01 to succeed${C_OFF}
    - Every project hostname resolves to the platform's public IP
    - :80 reachable from the public internet (Let's Encrypt's validation
      servers won't follow VPN / IP allow-lists)

EOF
