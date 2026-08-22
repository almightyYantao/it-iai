#!/usr/bin/env bash
# install-tls.sh — wire a wildcard TLS cert into Traefik and turn on
# automatic HTTP → HTTPS redirect for every user app.
#
# Run on the PLATFORM node:
#     sudo /opt/it-iai/deploy/install-tls.sh \
#         --cert /opt/it-iai/tls/example.com.crt \
#         --key  /opt/it-iai/tls/example.com.key
#
# With no flags the script looks under /opt/it-iai/tls/ for the first
# matching .crt + .key pair, which covers the common case.
#
# What it does:
#   1. Creates a kubernetes.io/tls Secret in kube-system
#   2. Creates a Traefik TLSStore named `default` pointing at the secret —
#      makes the wildcard the implicit cert for every TLS handshake that
#      doesn't have an explicit Ingress TLS section
#   3. Rewrites traefik-config.yaml (HelmChartConfig) to add a 80→443
#      redirect on the `web` entrypoint, so HTTP clients get bounced to
#      HTTPS automatically — no per-ingress TLS spec needed
#   4. Waits for Traefik to roll out the new config, then probes the
#      auth host over HTTPS to confirm the cert is being served
#
# Idempotent. Safe to re-run after rotating the cert.

set -uo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"

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

# --- args ------------------------------------------------------------------

CERT_PATH=""
KEY_PATH=""
SECRET_NAME="${SECRET_NAME:-iai-wildcard-tls}"
NAMESPACE="${NAMESPACE:-kube-system}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cert)   CERT_PATH="$2"; shift 2 ;;
    --key)    KEY_PATH="$2";  shift 2 ;;
    --secret) SECRET_NAME="$2"; shift 2 ;;
    -h|--help)
      cat <<EOF
Usage: $0 [--cert PATH] [--key PATH] [--secret NAME]

  --cert PATH    fullchain certificate (PEM). Default: first *.crt or *.pem
                 under /opt/it-iai/tls/.
  --key PATH     private key (PEM). Default: first *.key under /opt/it-iai/tls/.
  --secret NAME  K8s Secret name. Default: iai-wildcard-tls.

Env: NAMESPACE (default kube-system), KUBECONFIG.
EOF
      exit 0 ;;
    *) die "unknown flag: $1" ;;
  esac
done

# Auto-discovery when paths weren't passed in.
if [[ -z "$CERT_PATH" ]]; then
  for f in /opt/it-iai/tls/*.crt /opt/it-iai/tls/*.pem; do
    [[ -f "$f" ]] || continue
    # Skip files that look like issuer-only chains (no leaf inside).
    [[ "$f" == *_issuer* || "$f" == *issuerCertificate* ]] && continue
    CERT_PATH="$f"; break
  done
fi
if [[ -z "$KEY_PATH" ]]; then
  for f in /opt/it-iai/tls/*.key; do
    [[ -f "$f" ]] || continue
    KEY_PATH="$f"; break
  done
fi

[[ -r "$CERT_PATH" ]] || die "cert not readable: ${CERT_PATH:-<unset>}"
[[ -r "$KEY_PATH"  ]] || die "key not readable:  ${KEY_PATH:-<unset>}"

# --- sanity-check the pair -------------------------------------------------

# Two cheap pre-flight checks before pushing into the cluster:
#   1. cert + key share a public key
#   2. cert is a fullchain (has more than one BEGIN CERTIFICATE block) —
#      otherwise browsers will scream "incomplete certificate chain"
cert_pub=$(openssl x509 -in "$CERT_PATH" -pubkey -noout 2>/dev/null | openssl sha256 2>/dev/null | awk '{print $NF}' || true)
key_pub=$(openssl pkey  -in "$KEY_PATH" -pubout 2>/dev/null | openssl sha256 2>/dev/null | awk '{print $NF}' || true)
if [[ -z "$cert_pub" || -z "$key_pub" ]]; then
  warn "couldn't extract public-key fingerprint — skipping pair check"
elif [[ "$cert_pub" != "$key_pub" ]]; then
  die "cert and key don't match! $cert_pub vs $key_pub"
else
  ok "cert + key match"
fi

cert_blocks=$(grep -c "BEGIN CERTIFICATE" "$CERT_PATH" 2>/dev/null || echo 0)
if (( cert_blocks < 2 )); then
  warn "cert file has only $cert_blocks BEGIN CERTIFICATE block(s) — looks like a leaf only, browsers may show 'incomplete chain'. Use the fullchain (leaf + intermediates) file instead."
else
  ok "fullchain detected ($cert_blocks cert blocks in file)"
fi

# Subject Alternative Names — surface what the cert actually covers.
sans=$(openssl x509 -in "$CERT_PATH" -noout -ext subjectAltName 2>/dev/null \
  | tr ',' '\n' | grep -oE 'DNS:[^,[:space:]]+' | sed 's/^DNS://' | paste -sd, -)
[[ -n "$sans" ]] && info "cert covers: $sans"

cat <<EOF

${C_BOLD}Installing TLS into Traefik${C_OFF}
  ${C_DIM}cert         :${C_OFF} $CERT_PATH
  ${C_DIM}key          :${C_OFF} $KEY_PATH
  ${C_DIM}secret name  :${C_OFF} $SECRET_NAME
  ${C_DIM}namespace    :${C_OFF} $NAMESPACE
EOF

# --- 1. create / refresh the Secret ----------------------------------------

info "creating/updating Secret $NAMESPACE/$SECRET_NAME"
# `kubectl create secret tls --dry-run -o yaml | kubectl apply -f -` is the
# idempotent pattern — `create` alone fails when the Secret exists, `apply`
# alone doesn't accept --cert/--key.
"${KUBECTL[@]}" create secret tls "$SECRET_NAME" \
  --cert="$CERT_PATH" --key="$KEY_PATH" \
  -n "$NAMESPACE" --dry-run=client -o yaml \
  | "${KUBECTL[@]}" apply -f - >/dev/null
ok "Secret $NAMESPACE/$SECRET_NAME applied"

# --- 2. TLSStore so Traefik picks up the cert as default ------------------

info "applying default TLSStore → $SECRET_NAME"
cat <<EOF | "${KUBECTL[@]}" apply -f - >/dev/null
apiVersion: traefik.io/v1alpha1
kind: TLSStore
metadata:
  name: default
  namespace: $NAMESPACE
spec:
  defaultCertificate:
    secretName: $SECRET_NAME
EOF
ok "TLSStore/default applied"

# --- 3. rewrite Traefik HelmChartConfig with HTTP → HTTPS redirect --------
#
# We REPLACE the entire HelmChartConfig with a known-good values block so the
# net result is deterministic (vs trying to merge-patch). The values mirror
# what install-platform.sh writes plus the `redirections` stanza.
#
# Because this is a wholesale replace, EVERY setting the cluster needs has to be
# represented below — anything missing is silently dropped on re-run. That bit
# us once already: `providers.kubernetesCRD.allowCrossNamespace` was applied by
# hand for per-project path rules (a16a9e2) and never folded back in here, so a
# routine cert rotation would have taken out token auth on every project with a
# rule. Its IngressRoute references the cluster-wide Middleware
# oauth2-proxy/iai-api-token-verify, and Traefik rejects a cross-namespace
# Middleware reference without that flag. Keep the block below in sync with
# install-platform.sh, and add to it rather than hand-patching the cluster.

info "patching Traefik HelmChartConfig (adds web → websecure redirect)"
mkdir -p /var/lib/rancher/k3s/server/manifests
cat > /var/lib/rancher/k3s/server/manifests/traefik-config.yaml <<'YAML'
apiVersion: helm.cattle.io/v1
kind: HelmChartConfig
metadata:
  name: traefik
  namespace: kube-system
spec:
  valuesContent: |-
    providers:
      kubernetesCRD:
        allowCrossNamespace: true
    deployment:
      kind: DaemonSet
      # hostNetwork: true (below) makes the pod use the host's
      # /etc/resolv.conf by default, which on cloud VMs points at provider
      # DNS (e.g. 100.100.2.136 on Aliyun) that can't resolve
      # *.svc.cluster.local. Result: ForwardAuth/errors middleware that
      # references a cross-namespace Service (oauth2-proxy.oauth2-proxy)
      # fails with "no such host" and Traefik returns 500. Force the pod
      # to query CoreDNS first.
      dnsPolicy: ClusterFirstWithHostNet
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
        # NEW vs install-platform.sh: bounce every HTTP request to HTTPS so
        # we don't have to add TLS sections per-Ingress.
        redirections:
          entryPoint:
            to: websecure
            scheme: https
            permanent: true
      websecure:
        port: 443
        exposedPort: 443
      traefik:
        port: 9099
        exposedPort: 9099
      metrics:
        port: 9101
        exposedPort: 9101
    securityContext:
      capabilities:
        add: [NET_BIND_SERVICE]
        drop: [ALL]
      runAsUser: 0
      runAsNonRoot: false
YAML
ok "wrote /var/lib/rancher/k3s/server/manifests/traefik-config.yaml"

# --- 4. wait for rollout + probe ------------------------------------------

info "waiting up to 90s for Traefik to roll out (helm-controller reconciles ~30-60s)"
deadline=$(( $(date +%s) + 90 ))
while (( $(date +%s) < deadline )); do
  if "${KUBECTL[@]}" -n "$NAMESPACE" rollout status ds/traefik --timeout=15s >/dev/null 2>&1; then
    ok "Traefik DaemonSet ready"
    break
  fi
  sleep 5
done

cat <<EOF

${C_BOLD}Verify${C_OFF}

  # 1. From the platform node — should print the wildcard cert subject
  echo | openssl s_client -connect 127.0.0.1:443 -servername auth.example.com 2>/dev/null \
    | openssl x509 -noout -subject -issuer -dates

  # 2. HTTP → HTTPS redirect (Location header should be https://...)
  curl -sI http://auth.example.com/oauth2/sign_in | grep -iE '^(HTTP|Location)'

  # 3. Real login page over HTTPS (expect 200 or 302 to Keycloak)
  curl -kI https://auth.example.com/oauth2/sign_in

${C_BOLD}If something looks wrong${C_OFF}
  - Traefik logs:   ${KUBECTL[*]} -n $NAMESPACE logs -l app.kubernetes.io/name=traefik --tail=80
  - Reconcile:      ${KUBECTL[*]} -n $NAMESPACE get helmchart,helmchartconfig
EOF
