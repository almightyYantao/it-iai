#!/usr/bin/env bash
# deploy +share — manage project collaborators.
#
#   bash skill/scripts/share.sh list
#   bash skill/scripts/share.sh add alice@example.com
#   bash skill/scripts/share.sh remove alice@example.com

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
source "$HERE/lib/common.sh"

cmd="${1-list}"
email="${2-}"

slug=$(vd_state_get slug || true)
[[ -z "$slug" ]] && vd_die "no slug for cwd; run \`deploy +push\` first or pass --slug"

case "$cmd" in
  list)
    vd_api GET "/v1/projects/$slug/collaborators" | jq -r '
      .collaborators[]? | "\(.email)\t\(.role)\t\(.added_at)"
    ' | column -t -s $'\t'
    ;;
  add)
    [[ -z "$email" ]] && vd_die "usage: deploy +share add <email>"
    body=$(mktemp); jq -n --arg e "$email" '{email: $e}' > "$body"
    vd_api POST "/v1/projects/$slug/collaborators" "$body" >/dev/null
    rm -f "$body"
    vd_info "added $email as editor"
    ;;
  remove|rm)
    [[ -z "$email" ]] && vd_die "usage: deploy +share remove <email>"
    vd_api DELETE "/v1/projects/$slug/collaborators/$email" >/dev/null
    vd_info "removed $email"
    ;;
  *)
    vd_die "unknown subcommand: $cmd (try: list / add / remove)"
    ;;
esac
