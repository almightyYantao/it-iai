#!/usr/bin/env bash
# iai Skill — one-click installer / checker / deployer.
#
# Recommended one-liner for new teammates (idempotent — safe to re-run for upgrades):
#
#   rm -rf ~/iai && git clone https://github.com/almightyYantao/it-iai.git ~/iai \
#     && bash ~/iai/skill/install.sh install
#
# Usage:
#   ./install.sh install     # idempotent install (recommended)
#   ./install.sh             # auto-mode: install on fresh systems, check on existing ones
#   ./install.sh check       # diagnose existing install (no changes)
#   ./install.sh deploy      # push current dir (alias for skill push)
#   ./install.sh uninstall   # remove the skill symlink (config + creds preserved)
#
# Why this script exists: people don't read multi-step tutorials. They paste
# one line, want to see it work, and only then care about the moving parts.
# This bundles dependency check, skill registration, credential write, and
# connectivity verification so the happy path is one command.

set -euo pipefail

# Re-exec under bash if the user invoked us with sh/dash. We use bashisms
# (arrays, [[ ]]) all over and the rest of the skill needs bash anyway.
if [ -z "${BASH_VERSION:-}" ]; then
  exec bash "$0" "$@"
fi

# --- styling ---------------------------------------------------------------
# ANSI-C $'...' quoting; works regardless of -e support in echo.

if [[ -t 1 ]]; then
  C_OK=$'\033[32m'
  C_INFO=$'\033[36m'
  C_WARN=$'\033[33m'
  C_ERR=$'\033[31m'
  C_DIM=$'\033[2m'
  C_BOLD=$'\033[1m'
  C_OFF=$'\033[0m'
else
  C_OK="" C_INFO="" C_WARN="" C_ERR="" C_DIM="" C_BOLD="" C_OFF=""
fi

ok()    { printf '%s✓%s %s\n'  "$C_OK"   "$C_OFF" "$*"; }
info()  { printf '%s•%s %s\n'  "$C_INFO" "$C_OFF" "$*"; }
warn()  { printf '%s!%s %s\n'  "$C_WARN" "$C_OFF" "$*"; }
err()   { printf '%s✗%s %s\n'  "$C_ERR"  "$C_OFF" "$*" >&2; }
dim()   { printf '%s%s%s\n'    "$C_DIM"  "$*"    "$C_OFF"; }
title() { printf '\n%s%s%s\n'  "$C_BOLD" "$*"    "$C_OFF"; }

die() { err "$*"; exit 1; }

# --- locate skill source --------------------------------------------------
# Resolve the directory containing THIS script — that's the skill root.
# Robust against symlinks, spaces in path, and running via curl|bash pipe
# (in which case BASH_SOURCE is the temp path and we abort with a hint).

resolve_self() {
  local src="${BASH_SOURCE[0]}"
  if [[ -z "$src" || "$src" == "bash" || "$src" == "-bash" ]]; then
    err "this script must be run as a file, not piped through bash."
    err "clone the repo first, then: bash skill/install.sh"
    exit 1
  fi
  while [[ -L "$src" ]]; do
    local d
    d=$(cd "$(dirname "$src")" && pwd)
    src=$(readlink "$src")
    [[ "$src" != /* ]] && src="$d/$src"
  done
  cd "$(dirname "$src")" && pwd
}

SKILL_ROOT=$(resolve_self)

# Quick sanity check: this is actually the skill directory.
if [[ ! -f "$SKILL_ROOT/SKILL.md" || ! -d "$SKILL_ROOT/scripts" ]]; then
  die "expected SKILL.md and scripts/ next to this installer (got $SKILL_ROOT)"
fi

# --- paths -----------------------------------------------------------------

CLAUDE_SKILLS_DIR="${CLAUDE_SKILLS_DIR:-$HOME/.claude/skills}"
SKILL_LINK="$CLAUDE_SKILLS_DIR/iai"
IAI_HOME="${VIBEDEPLOY_HOME:-$HOME/.vibedeploy}"
CONFIG_FILE="$IAI_HOME/config.json"
CRED_FILE="$IAI_HOME/credentials.json"

# --- deps ------------------------------------------------------------------

REQUIRED_CMDS=(bash curl jq tar zstd git)

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "macos" ;;
    Linux)  echo "linux" ;;
    *)      echo "other" ;;
  esac
}

install_deps_macos() {
  if ! command -v brew >/dev/null 2>&1; then
    warn "Homebrew not found — install it from https://brew.sh, then retry."
    return 1
  fi
  local missing=()
  for c in jq zstd; do
    command -v "$c" >/dev/null 2>&1 || missing+=("$c")
  done
  if (( ${#missing[@]} > 0 )); then
    info "installing via brew: ${missing[*]}"
    brew install "${missing[@]}"
  fi
}

install_deps_linux() {
  local missing=()
  for c in jq zstd curl tar git; do
    command -v "$c" >/dev/null 2>&1 || missing+=("$c")
  done
  (( ${#missing[@]} == 0 )) && return 0

  local installer=""
  if command -v apt-get >/dev/null 2>&1; then installer="apt-get install -y"; fi
  if command -v dnf      >/dev/null 2>&1; then installer="dnf install -y";    fi
  if command -v yum      >/dev/null 2>&1; then installer="yum install -y";    fi
  if command -v apk      >/dev/null 2>&1; then installer="apk add";           fi
  if [[ -z "$installer" ]]; then
    warn "no supported package manager (apt/dnf/yum/apk) — install manually: ${missing[*]}"
    return 1
  fi
  info "installing via system package manager: ${missing[*]}"
  if [[ $EUID -eq 0 ]]; then
    # shellcheck disable=SC2086
    $installer ${missing[*]}
  else
    # shellcheck disable=SC2086
    sudo $installer ${missing[*]}
  fi
}

check_deps() {
  local missing=()
  for c in "${REQUIRED_CMDS[@]}"; do
    if ! command -v "$c" >/dev/null 2>&1; then
      missing+=("$c")
    fi
  done
  if (( ${#missing[@]} > 0 )); then
    return 1
  fi
  return 0
}

ensure_deps() {
  if check_deps; then
    ok "all dependencies present: ${REQUIRED_CMDS[*]}"
    return 0
  fi
  case "$(detect_os)" in
    macos) install_deps_macos || true ;;
    linux) install_deps_linux || true ;;
    *)     warn "unknown OS — install these manually: ${REQUIRED_CMDS[*]}" ;;
  esac
  if check_deps; then
    ok "dependencies installed"
  else
    err "still missing: $(for c in "${REQUIRED_CMDS[@]}"; do command -v "$c" >/dev/null 2>&1 || printf '%s ' "$c"; done)"
    die "install the listed tools and re-run"
  fi
}

# --- skill registration ----------------------------------------------------

link_skill() {
  mkdir -p "$CLAUDE_SKILLS_DIR"

  # If something already lives at the target, decide whether it's our doing.
  if [[ -L "$SKILL_LINK" ]]; then
    local existing
    existing=$(readlink "$SKILL_LINK")
    if [[ "$existing" == "$SKILL_ROOT" ]]; then
      ok "skill symlink already up to date: $SKILL_LINK → $SKILL_ROOT"
      return 0
    fi
    warn "overwriting existing symlink: $SKILL_LINK"
    warn "  was → $existing"
    warn "  new → $SKILL_ROOT"
    rm -f "$SKILL_LINK"
  elif [[ -e "$SKILL_LINK" ]]; then
    # A real directory or file lives there — don't blow it away without asking.
    err "$SKILL_LINK exists and is NOT a symlink — refusing to overwrite."
    err "remove it manually if you want a fresh install: rm -rf '$SKILL_LINK'"
    return 1
  fi

  ln -s "$SKILL_ROOT" "$SKILL_LINK"
  ok "registered skill: $SKILL_LINK → $SKILL_ROOT"
}

# --- config / credentials --------------------------------------------------

prompt_api_url() {
  local current
  current=$(jq -r '.api_url // empty' "$CONFIG_FILE" 2>/dev/null || true)
  local default="${current:-${VIBEDEPLOY_API:-http://localhost:8080}}"
  if [[ ! -t 0 ]]; then
    echo "$default"
    return 0
  fi
  local answer
  printf '%s Control-plane API URL [%s%s%s]: ' "${C_INFO}•${C_OFF}" "$C_DIM" "$default" "$C_OFF" >&2
  read -r answer || true
  echo "${answer:-$default}"
}

prompt_token() {
  if [[ -n "${VIBEDEPLOY_TOKEN:-}" ]]; then
    echo "$VIBEDEPLOY_TOKEN"
    return 0
  fi
  if [[ -f "$CRED_FILE" ]]; then
    local existing
    existing=$(jq -r '.token // .access_token // empty' "$CRED_FILE" 2>/dev/null || true)
    if [[ -n "$existing" ]]; then
      echo "$existing"
      return 0
    fi
  fi
  if [[ ! -t 0 ]]; then
    return 0
  fi
  printf '%s Deploy Token (vbd_live_...): ' "${C_INFO}•${C_OFF}" >&2
  local tok
  read -rs tok || true
  echo >&2
  echo "$tok"
}

write_config() {
  local api_url="$1"
  mkdir -p "$IAI_HOME"
  local tmp
  tmp=$(mktemp)
  if [[ -f "$CONFIG_FILE" ]]; then
    jq --arg u "$api_url" '.api_url = $u' "$CONFIG_FILE" > "$tmp"
  else
    jq -n --arg u "$api_url" '{api_url: $u}' > "$tmp"
  fi
  mv "$tmp" "$CONFIG_FILE"
  chmod 0600 "$CONFIG_FILE"
  ok "wrote $CONFIG_FILE"
}

write_token() {
  local tok="$1"
  [[ -z "$tok" ]] && { warn "no token captured — set VIBEDEPLOY_TOKEN later"; return 0; }
  mkdir -p "$IAI_HOME"
  jq -n --arg t "$tok" '{token: $t}' > "$CRED_FILE"
  chmod 0600 "$CRED_FILE"
  ok "wrote $CRED_FILE (mode 0600)"
}

# --- connectivity check ----------------------------------------------------

probe_healthz() {
  local api_url="$1"
  local code
  code=$(curl -fsS -m 6 -o /dev/null -w '%{http_code}' "${api_url%/}/healthz" 2>/dev/null || echo "000")
  echo "$code"
}

probe_whoami() {
  local api_url="$1" tok="$2"
  [[ -z "$tok" ]] && { echo "skip"; return 0; }
  local code
  code=$(curl -fsS -m 8 -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer $tok" \
    "${api_url%/}/v1/whoami" 2>/dev/null || echo "000")
  echo "$code"
}

# --- commands --------------------------------------------------------------

cmd_install() {
  title "Installing iai Skill"
  dim "  skill source : $SKILL_ROOT"
  dim "  install link : $SKILL_LINK"
  dim "  home         : $IAI_HOME"

  ensure_deps
  link_skill

  local api_url tok
  api_url=$(prompt_api_url)
  write_config "$api_url"

  tok=$(prompt_token || true)
  write_token "$tok"

  # Probe.
  local hc wc
  hc=$(probe_healthz "$api_url")
  case "$hc" in
    200) ok "control-plane reachable at $api_url" ;;
    000) warn "couldn't connect to $api_url — check the host is up and you're on the right network" ;;
    *)   warn "control-plane $api_url returned HTTP $hc on /healthz" ;;
  esac
  wc=$(probe_whoami "$api_url" "$tok")
  case "$wc" in
    skip) warn "no token configured — run: deploy +login" ;;
    200)  ok "authenticated (/v1/whoami)" ;;
    401|403) err "token rejected (HTTP $wc) — get a fresh one from the platform host" ;;
    000)  ;; # already warned above
    *)    warn "/v1/whoami returned HTTP $wc" ;;
  esac

  title "Next"
  cat <<EOF
  1) Restart Claude Code so it picks up the new skill.
  2) cd into any project, then in Claude Code say: "${C_BOLD}部署一下${C_OFF}" or "${C_BOLD}deploy +push${C_OFF}".
  3) To diagnose later:  ${C_BOLD}$(basename "$0") check${C_OFF}
EOF
}

cmd_check() {
  title "iai Skill — health check"

  # Deps
  if check_deps; then
    ok "dependencies: ${REQUIRED_CMDS[*]}"
  else
    err "missing dependencies — run: $(basename "$0") install"
  fi

  # Skill link
  if [[ -L "$SKILL_LINK" ]]; then
    local target
    target=$(readlink "$SKILL_LINK")
    if [[ "$target" == "$SKILL_ROOT" ]]; then
      ok "skill registered: $SKILL_LINK"
    else
      warn "skill link points elsewhere: $target (expected $SKILL_ROOT)"
    fi
  elif [[ -d "$SKILL_LINK" ]]; then
    warn "skill path exists as a directory (not symlink) — Claude Code will still see it but updates won't auto-pull"
  else
    err "skill not registered: $SKILL_LINK missing — run: $(basename "$0") install"
  fi

  # Config
  if [[ -f "$CONFIG_FILE" ]]; then
    local api_url
    api_url=$(jq -r '.api_url // empty' "$CONFIG_FILE" 2>/dev/null || true)
    if [[ -n "$api_url" ]]; then
      ok "config: api_url=$api_url"
      local hc
      hc=$(probe_healthz "$api_url")
      case "$hc" in
        200) ok "control-plane reachable" ;;
        000) err "unreachable: $api_url (network / DNS / firewall)" ;;
        *)   warn "control-plane responded HTTP $hc" ;;
      esac
    else
      warn "$CONFIG_FILE has no api_url"
    fi
  else
    warn "no $CONFIG_FILE — run: $(basename "$0") install"
  fi

  # Token
  local tok=""
  if [[ -n "${VIBEDEPLOY_TOKEN:-}" ]]; then
    tok="$VIBEDEPLOY_TOKEN"
    ok "token: from VIBEDEPLOY_TOKEN env"
  elif [[ -f "$CRED_FILE" ]]; then
    tok=$(jq -r '.token // .access_token // empty' "$CRED_FILE" 2>/dev/null || true)
    if [[ -n "$tok" ]]; then
      ok "token: from $CRED_FILE"
    else
      warn "$CRED_FILE present but no token field"
    fi
  else
    err "no token (no env, no $CRED_FILE) — run: deploy +login"
  fi

  if [[ -f "$CONFIG_FILE" && -n "$tok" ]]; then
    local api_url wc
    api_url=$(jq -r '.api_url // empty' "$CONFIG_FILE" 2>/dev/null || true)
    wc=$(probe_whoami "$api_url" "$tok")
    case "$wc" in
      200) ok "whoami OK" ;;
      401|403) err "token rejected by /v1/whoami (HTTP $wc)" ;;
      000)  ;; # network already reported above
      *) warn "/v1/whoami returned HTTP $wc" ;;
    esac
  fi
}

cmd_deploy() {
  exec bash "$SKILL_ROOT/scripts/push.sh" "$@"
}

cmd_uninstall() {
  title "Uninstalling iai Skill"
  if [[ -L "$SKILL_LINK" ]]; then
    rm -f "$SKILL_LINK"
    ok "removed symlink: $SKILL_LINK"
  else
    info "no symlink at $SKILL_LINK"
  fi
  warn "credentials and config preserved at $IAI_HOME (delete manually if you want a clean slate)"
}

usage() {
  cat <<EOF
Usage: $(basename "$0") [install|check|deploy|uninstall]

  install     Idempotent install: deps + skill symlink + config + credentials + probe
  check       Diagnose an existing install — read-only
  deploy      Push current directory (alias for the skill's push.sh)
  uninstall   Remove the symlink from ~/.claude/skills/ (keeps config + creds)

  No args: equivalent to 'install' on a fresh system, 'check' otherwise.
EOF
}

# --- entrypoint ------------------------------------------------------------

case "${1:-auto}" in
  install)   cmd_install ;;
  check)     cmd_check ;;
  deploy)    shift; cmd_deploy "$@" ;;
  uninstall) cmd_uninstall ;;
  -h|--help|help) usage ;;
  auto)
    # Heuristic: if the symlink already points at us AND deps are present,
    # the user probably wants to re-check rather than re-install.
    if [[ -L "$SKILL_LINK" && "$(readlink "$SKILL_LINK")" == "$SKILL_ROOT" ]] && check_deps; then
      cmd_check
    else
      cmd_install
    fi
    ;;
  *) err "unknown command: $1"; usage; exit 1 ;;
esac
