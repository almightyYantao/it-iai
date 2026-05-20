#!/usr/bin/env bash
# Shared helpers for VibeDeploy Skill scripts.
# Source this from any command script:  source "$(dirname "$0")/lib/common.sh"

set -euo pipefail

VIBEDEPLOY_HOME="${VIBEDEPLOY_HOME:-$HOME/.vibedeploy}"
CONFIG_FILE="$VIBEDEPLOY_HOME/config.json"
CRED_FILE="$VIBEDEPLOY_HOME/credentials.json"

mkdir -p "$VIBEDEPLOY_HOME"

# --- logging ---------------------------------------------------------------

vd_info()  { printf '\033[36m✓\033[0m %s\n' "$*" >&2; }
vd_warn()  { printf '\033[33m!\033[0m %s\n' "$*" >&2; }
vd_err()   { printf '\033[31m✗\033[0m %s\n' "$*" >&2; }
vd_dim()   { printf '\033[2m%s\033[0m\n' "$*" >&2; }

vd_die()   { vd_err "$*"; exit 1; }

# --- dependency check ------------------------------------------------------

vd_need() {
  command -v "$1" >/dev/null 2>&1 || vd_die "missing dependency: $1"
}
vd_need curl
vd_need jq
vd_need tar
vd_need zstd

# --- config ---------------------------------------------------------------

vd_config_get() {
  local key="$1" default="${2-}"
  if [[ -f "$CONFIG_FILE" ]]; then
    local val
    val=$(jq -r --arg k "$key" '.[$k] // empty' "$CONFIG_FILE")
    [[ -n "$val" ]] && { echo "$val"; return 0; }
  fi
  echo "$default"
}

vd_config_set() {
  local key="$1" value="$2"
  local tmp
  tmp=$(mktemp)
  if [[ -f "$CONFIG_FILE" ]]; then
    jq --arg k "$key" --arg v "$value" '.[$k] = $v' "$CONFIG_FILE" > "$tmp"
  else
    jq -n --arg k "$key" --arg v "$value" '{($k): $v}' > "$tmp"
  fi
  mv "$tmp" "$CONFIG_FILE"
  chmod 0600 "$CONFIG_FILE"
}

vd_api_url() {
  vd_config_get api_url "${VIBEDEPLOY_API:-http://localhost:8080}"
}

# Prompt the user for a missing API URL when running in a TTY.
# Without a TTY (CI, piped Claude tool call) we fall back to the env-driven
# default rather than hanging on stdin.
vd_ensure_api_url() {
  local cur
  cur=$(vd_config_get api_url "")
  if [[ -n "$cur" ]]; then
    echo "$cur"
    return 0
  fi
  if [[ -n "${VIBEDEPLOY_API:-}" ]]; then
    vd_config_set api_url "$VIBEDEPLOY_API"
    echo "$VIBEDEPLOY_API"
    return 0
  fi
  if [[ ! -t 0 ]]; then
    # No interactive shell — keep the localhost default and let the request fail
    # noisily so the caller sees a real error rather than a half-configured run.
    echo "http://localhost:8080"
    return 0
  fi
  local def="http://localhost:8080"
  printf '\033[36m?\033[0m Control-plane API URL\n' >&2
  printf '  \033[2m示例: https://admin.iai.your-company.com  /  http://10.0.0.5:8080\033[0m\n' >&2
  printf '  \033[2m向 IT 同事或部署负责人要这个地址\033[0m\n' >&2
  printf '\033[36m?\033[0m [\033[2m%s\033[0m]: ' "$def" >&2
  local ans
  read -r ans || true
  ans="${ans:-$def}"
  vd_config_set api_url "$ans"
  vd_info "saved api_url=$ans"
  echo "$ans"
}

# --- token ---------------------------------------------------------------

vd_token() {
  if [[ -n "${VIBEDEPLOY_TOKEN:-}" ]]; then
    echo "$VIBEDEPLOY_TOKEN"
    return 0
  fi
  if [[ -f "$CRED_FILE" ]]; then
    local tok
    tok=$(jq -r '.access_token // .token // empty' "$CRED_FILE")
    [[ -n "$tok" ]] && { echo "$tok"; return 0; }
  fi
  # No token yet — prompt the user inline if we have a TTY. The previous
  # behaviour ("run deploy +login first") shipped users to a separate command
  # that just prompts for the same string, which is friction we can remove.
  if [[ -t 0 ]]; then
    # Tell the user where to get the token. The /skill page on the Admin UI
    # has a one-click "copy export VIBEDEPLOY_TOKEN=…" line; api_url is
    # usually the same host as the Admin UI in single-domain deployments,
    # but we hint both possibilities to cover separated setups.
    local api
    api=$(vd_api_url)
    printf '\033[36m?\033[0m Paste your Deploy Token (vbd_live_...)\n' >&2
    printf '  \033[2m在 Admin UI 的 /skill 页面生成（明文只显示一次）\033[0m\n' >&2
    printf '  \033[2m地址例: %s/skill  （如果 Admin UI 和 API 不同域，请改成 admin.<your-domain>/skill）\033[0m\n' >&2
    printf '\033[36m?\033[0m Token: ' >&2
    local tok
    read -rs tok || true
    echo >&2
    if [[ -n "$tok" ]]; then
      vd_save_token "$tok"
      echo "$tok"
      return 0
    fi
  fi
  vd_die "no token. Set VIBEDEPLOY_TOKEN=vbd_... or run: deploy +login"
}

vd_save_token() {
  local tok="$1"
  jq -n --arg t "$tok" '{token: $t}' > "$CRED_FILE"
  chmod 0600 "$CRED_FILE"
}

# --- API ------------------------------------------------------------------

# vd_api METHOD PATH [BODY_FILE]
# Echoes response body to stdout. Returns non-zero on:
#   - HTTP status >= 400
#   - connection failure (curl writes 000 to %{http_code} when it can't connect)
#   - empty status (caught for safety; should not happen but better safe)
# All diagnostics — including curl's stderr — go to our stderr.
vd_api() {
  local method="$1" path="$2" body="${3-}"
  local url base
  base=$(vd_api_url)
  url="${base%/}${path}"
  local tok
  tok=$(vd_token)
  local tmp_resp tmp_status
  tmp_resp=$(mktemp); tmp_status=$(mktemp)
  local -a curl_args=(
    -sS
    -X "$method"
    -H "Authorization: Bearer $tok"
    -H "Content-Type: application/json"
    -H "Accept: application/json"
    -o "$tmp_resp"
    -w '%{http_code}'
  )
  if [[ -n "$body" ]]; then
    curl_args+=(--data-binary "@$body")
  fi
  local code
  code=$(curl "${curl_args[@]}" "$url" 2>"$tmp_status" || true)

  # 000 == curl couldn't connect (DNS / refused / TLS handshake / etc.)
  # Numeric compare on "000" silently passes a < 400 check, so be explicit.
  if [[ -z "$code" || "$code" == "000" || "$code" -ge 400 ]]; then
    vd_err "API $method $path -> ${code:-no-response}"
    if [[ -s "$tmp_status" ]]; then
      sed 's/^/    curl: /' "$tmp_status" >&2
    fi
    if [[ -s "$tmp_resp" ]]; then
      sed 's/^/    body: /' "$tmp_resp" >&2
    fi
    if [[ "$code" == "000" || -z "$code" ]]; then
      vd_warn "is the Control Plane reachable at $base ?"
    fi
    rm -f "$tmp_resp" "$tmp_status"
    return 1
  fi
  cat "$tmp_resp"
  rm -f "$tmp_resp" "$tmp_status"
}

# --- project state in cwd ------------------------------------------------

vd_state_file() { echo "$(pwd)/.vibedeploy.json"; }

vd_state_get() {
  local key="$1"
  local f
  f=$(vd_state_file)
  [[ -f "$f" ]] || { echo ""; return 0; }
  jq -r --arg k "$key" '.[$k] // empty' "$f"
}

vd_state_set() {
  local key="$1" value="$2"
  local f tmp
  f=$(vd_state_file)
  tmp=$(mktemp)
  if [[ -f "$f" ]]; then
    jq --arg k "$key" --arg v "$value" '.[$k] = $v' "$f" > "$tmp"
  else
    jq -n --arg k "$key" --arg v "$value" '{($k): $v}' > "$tmp"
  fi
  mv "$tmp" "$f"
}
