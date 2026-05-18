#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
source "$HERE/lib/common.sh"

slug="${1-}"
if [[ -z "$slug" ]]; then
  slug=$(vd_state_get slug || true)
fi
[[ -z "$slug" ]] && vd_die "no slug. pass one (deploy +status SLUG) or run +push first in this directory."

proj=$(vd_api GET "/v1/projects/$slug")
echo "$proj" | jq '.project | {slug, name, status, visibility, url, last_pushed_at, last_active_at}'

# Latest deployment.
deps=$(vd_api GET "/v1/projects/$slug/deployments?limit=1")
echo "$deps" | jq '.deployments[0] | {id, status, image_tag, created_at, deployed_at, failure_reason}'
