#!/usr/bin/env bash
# deploy +login — M1 dev mode just asks for a Deploy Token.
# M2 will swap in Keycloak device flow.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
source "$HERE/lib/common.sh"

API_URL="${1-}"
if [[ -n "$API_URL" ]]; then
  vd_config_set api_url "$API_URL"
  vd_info "saved api_url=$API_URL"
fi

if [[ -n "${VIBEDEPLOY_TOKEN:-}" ]]; then
  vd_info "using VIBEDEPLOY_TOKEN from environment"
else
  if [[ ! -t 0 ]]; then
    cat >&2 <<'HINT'
[31m✗[0m no VIBEDEPLOY_TOKEN and stdin is not a tty — can't prompt for one.

  Add these to your ~/.zshrc or ~/.bashrc (and reopen your shell):

    [33mexport VIBEDEPLOY_API=http://<control-plane-host>:8080[0m
    [33mexport VIBEDEPLOY_TOKEN=vbd_live_xxxxxxxxxxxx[0m

  Get the token from the control-plane host:
    [2mssh <platform-host> 'docker compose -f /opt/it-iai/docker-compose.yml \
      exec -T control-plane /control-plane seed-dev-token'[0m

  Then retry: [33mdeploy +login[0m
HINT
    exit 1
  fi
  echo -n "Paste your Deploy Token (vbd_live_...): " >&2
  read -rs token; echo >&2
  [[ -z "$token" ]] && vd_die "empty token"
  vd_save_token "$token"
fi

# Verify
who=$(vd_api GET /v1/whoami)
echo "$who" | jq .
vd_info "logged in"
