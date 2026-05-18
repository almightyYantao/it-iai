#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
source "$HERE/lib/common.sh"

follow=false
n=200
phase=build
slug=""
dep_id=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -f|--follow) follow=true; shift ;;
    -n) n="$2"; shift 2 ;;
    --runtime) phase=runtime; shift ;;
    --build)   phase=build;   shift ;;
    --dep)     dep_id="$2"; shift 2 ;;
    -*) vd_die "unknown flag: $1" ;;
    *)  slug="$1"; shift ;;
  esac
done

if [[ -z "$slug" ]]; then
  slug=$(vd_state_get slug || true)
fi
[[ -z "$slug" ]] && vd_die "no slug. pass one or run +push first."

if [[ -z "$dep_id" ]]; then
  dep_id=$(vd_api GET "/v1/projects/$slug/deployments?limit=1" | jq -r '.deployments[0].id // empty')
  [[ -z "$dep_id" ]] && vd_die "no deployment yet"
fi

if $follow; then
  tok=$(vd_token)
  base=$(vd_api_url)
  exec curl -N -sS \
    -H "Authorization: Bearer $tok" \
    -H "Accept: text/event-stream" \
    "${base%/}/v1/projects/$slug/deployments/$dep_id/events"
else
  vd_api GET "/v1/projects/$slug/deployments/$dep_id/logs?phase=$phase&n=$n"
fi
