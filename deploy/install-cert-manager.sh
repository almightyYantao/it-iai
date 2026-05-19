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
#   4. Installs a CoreDNS override (`coredns-custom` configmap) that
#      resolves *.<BASE_DOMAIN> to the platform node's INTERNAL IP from
#      inside the cluster. Works around cloud hairpin-NAT (Alibaba etc.
#      don't let an ECS reach its own public IP), which otherwise makes
#      cert-manager's HTTP-01 self-check time out. External DNS is
#      untouched — LE's validators still hit the public IP.
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

# BASE_DOMAIN drives the in-cluster DNS override that fixes the hairpin-NAT
# problem (see step 4). Prefer explicit env, fall back to .env file in the
# same repo. If we can't determine it the override is skipped with a warning
# — cert issuance will still work IF the cloud network allows ECS-to-own-
# public-IP loopback (rare).
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
BASE_DOMAIN="${BASE_DOMAIN:-}"
if [[ -z "$BASE_DOMAIN" && -f "$REPO_ROOT/.env" ]]; then
  BASE_DOMAIN=$(awk -F= '$1=="CP_APP_BASE_DOMAIN"{sub(/^[^=]*=/,""); print; exit}' "$REPO_ROOT/.env" || true)
fi

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
  ${C_DIM}base domain   :${C_OFF} ${BASE_DOMAIN:-<unknown — CoreDNS hairpin fix will be skipped>}

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

# --- 4. CoreDNS hairpin-NAT override --------------------------------------
#
# Cloud providers (Alibaba, Tencent, AWS without enableDnsHostnames) typically
# don't allow an ECS instance to reach its own public IP — the kernel sees the
# packet as coming-from-self and drops it. cert-manager's HTTP-01 self-check
# resolves the project hostname to the *public* IP via cluster DNS, the
# request goes out the node, hits the NAT, gets dropped, times out, and the
# challenge stays in `pending` forever.
#
# Fix: install a CoreDNS rewrite via the `coredns-custom` configmap that
# k3s' bundled CoreDNS imports automatically. Inside the cluster, every
# `*.<base-domain>` query returns the platform node's INTERNAL IP. From the
# pod, the request lands directly on Traefik's hostNetwork :80 — no NAT hop.
# External DNS (what Let's Encrypt actually validates against) is untouched.

if [[ -z "$BASE_DOMAIN" ]]; then
  warn "skipping CoreDNS hairpin fix — BASE_DOMAIN unknown."
  warn "If cert issuance hangs in 'pending' with self-check timeout, set"
  warn "BASE_DOMAIN=<your.domain> and re-run this script."
else
  PLATFORM_IP=$("${KUBECTL[@]}" get node -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null || true)
  if [[ -z "$PLATFORM_IP" ]]; then
    warn "couldn't read platform node InternalIP — skipping CoreDNS hairpin fix"
  else
    info "applying CoreDNS hairpin override for *.${BASE_DOMAIN} -> ${PLATFORM_IP}"

    # Refuse to clobber a pre-existing coredns-custom that has other keys.
    # Operators sometimes put their own overrides there; we shouldn't wipe them.
    OTHER_KEYS=$("${KUBECTL[@]}" -n kube-system get cm coredns-custom -o json 2>/dev/null \
      | grep -oE '"[^"]+":' | sed 's/[:"]//g' \
      | grep -vE '^(apiVersion|kind|metadata|name|namespace|data|labels|annotations|vibedeploy\.io/managed|iai-loopback\.override|creationTimestamp|resourceVersion|uid|managedFields|fieldsType|fieldsV1|f:.*|time|operation|manager|.*\..*\..*)$' \
      | sort -u || true)
    if [[ -n "$OTHER_KEYS" ]]; then
      warn "coredns-custom already has other keys: $(echo $OTHER_KEYS | tr '\n' ' ')"
      warn "Not overwriting. Add this manually under data: in coredns-custom:"
      warn "  iai-loopback.override: |"
      warn "    template IN A ${BASE_DOMAIN} {"
      warn "      match ^[^.]+\\.${BASE_DOMAIN//./\\.}\\.\$"
      warn "      answer \"{{ .Name }} 60 IN A ${PLATFORM_IP}\""
      warn "      fallthrough"
      warn "    }"
    else
      # Escape dots in BASE_DOMAIN for the regex match line.
      BASE_DOMAIN_RE="${BASE_DOMAIN//./\\.}"
      cat <<YAML | "${KUBECTL[@]}" apply -f - >/dev/null
apiVersion: v1
kind: ConfigMap
metadata:
  name: coredns-custom
  namespace: kube-system
  labels:
    vibedeploy.io/managed: "true"
data:
  iai-loopback.override: |
    template IN A ${BASE_DOMAIN} {
      match ^[^.]+\\.${BASE_DOMAIN_RE}\\.\$
      answer "{{ .Name }} 60 IN A ${PLATFORM_IP}"
      fallthrough
    }
YAML
      ok "coredns-custom applied"
      "${KUBECTL[@]}" -n kube-system rollout restart deploy coredns >/dev/null
      "${KUBECTL[@]}" -n kube-system rollout status  deploy coredns --timeout=60s >/dev/null && \
        ok "CoreDNS reloaded" || warn "CoreDNS rollout still in progress — check manually"
    fi
  fi
fi

# --- 5. summary ------------------------------------------------------------

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

  ${C_BOLD}Verify in-cluster DNS resolves to the internal IP${C_OFF}
    ${KUBECTL[*]} -n cert-manager exec deploy/cert-manager -- \\
      nslookup foo.${BASE_DOMAIN:-<base-domain>}
    # Should return the platform's INTERNAL IP, not the public IP.
    # If it returns the public IP, the hairpin fix isn't active —
    # check coredns-custom configmap and 'kubectl -n kube-system logs deploy/coredns'.

  ${C_BOLD}Prerequisites for HTTP-01 to succeed${C_OFF}
    - Every project hostname resolves (publicly) to the platform's public IP
    - :80 reachable from the public internet (Let's Encrypt's validation
      servers won't follow VPN / IP allow-lists)

EOF
