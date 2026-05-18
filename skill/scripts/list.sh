#!/usr/bin/env bash
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
source "$HERE/lib/common.sh"

resp=$(vd_api GET /v1/projects)
echo "$resp" | jq -r '
  .projects[]? as $p
  | "\($p.slug)\t\($p.status)\t\($p.url)"
' | column -t -s $'\t'
