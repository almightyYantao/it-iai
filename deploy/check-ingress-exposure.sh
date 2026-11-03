#!/usr/bin/env bash
# check-ingress-exposure.sh — run before opening 443 to the internet, and from cron.
#
#   * every native Ingress chains an IP allow-list (projects + admin-ui/oauth2-proxy);
#   * no IPAllowList middleware contains a /0;
#   * every IngressRoute route either inherits the project allow-list or, if it
#     uses a per-path allow-list (external sources), also has iai-api-token-verify.
#
# Exit code = number of problems.
set -uo pipefail
KUBECTL=(${KUBECTL:-k3s kubectl})
command -v jq >/dev/null || { echo "jq required" >&2; exit 99; }

problems=0
bad() { echo "FAIL  $*"; problems=$((problems + 1)); }

while IFS=$'\t' read -r ns name mw; do
  [[ "$mw" == *iai-allowlist* || "$mw" == *iai-internal-allowlist* ]] \
    || bad "ingress $ns/$name has no IP allow-list (middlewares: ${mw:-none})"
done < <("${KUBECTL[@]}" get ingress -A -o json | jq -r '.items[] |
  [.metadata.namespace, .metadata.name,
   (.metadata.annotations["traefik.ingress.kubernetes.io/router.middlewares"] // "")] | @tsv')

while IFS=$'\t' read -r ns name; do
  bad "middleware $ns/$name allows a /0 range"
done < <("${KUBECTL[@]}" get middleware -A -o json | jq -r '.items[] |
  select((.spec.ipAllowList.sourceRange // []) | any(test("/0$"))) |
  [.metadata.namespace, .metadata.name] | @tsv')

while IFS=$'\t' read -r ns name match reason; do
  bad "ingressroute $ns/$name [$match]: $reason"
done < <("${KUBECTL[@]}" get ingressroute -A -o json | jq -r '.items[] |
  .metadata as $m | .spec.routes[] |
  ([.middlewares[]?.name]) as $mws |
  if ($mws | map(startswith("iai-allowlist-")) | any | not) then
    [$m.namespace, $m.name, .match, "no IP allow-list"] | @tsv
  elif ($mws | map(test("-p[0-9a-f]{8}$")) | any) and ($mws | index("iai-api-token-verify") | not) then
    [$m.namespace, $m.name, .match, "external sources without token verify"] | @tsv
  else empty end')

echo "problems: $problems"
exit "$problems"
