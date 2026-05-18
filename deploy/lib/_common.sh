#!/usr/bin/env bash
# Shared helpers for VibeDeploy install scripts. Sourced — not run directly.
# shellcheck shell=bash

set -euo pipefail

vd_info()  { printf '\033[36m==>\033[0m %s\n' "$*" >&2; }
vd_ok()    { printf '\033[32m✓\033[0m %s\n'  "$*" >&2; }
vd_warn()  { printf '\033[33m!\033[0m %s\n'  "$*" >&2; }
vd_err()   { printf '\033[31m✗\033[0m %s\n'  "$*" >&2; }
vd_die()   { vd_err "$*"; exit 1; }

vd_require_root() {
  [[ ${EUID:-$(id -u)} -eq 0 ]] || vd_die "must run as root (use sudo)"
}

vd_detect_os() {
  if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    VD_OS_ID="${ID:-unknown}"
    VD_OS_VER="${VERSION_ID:-unknown}"
    VD_OS_FAMILY=""
    case "$VD_OS_ID" in
      ubuntu|debian)              VD_OS_FAMILY="debian" ;;
      rhel|centos|rocky|almalinux|fedora|amzn) VD_OS_FAMILY="rhel" ;;
      *) VD_OS_FAMILY="$VD_OS_ID" ;;
    esac
    export VD_OS_ID VD_OS_VER VD_OS_FAMILY
  else
    vd_die "cannot read /etc/os-release"
  fi
}

vd_pkg_install() {
  case "$VD_OS_FAMILY" in
    debian)
      DEBIAN_FRONTEND=noninteractive apt-get update -y
      DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"
      ;;
    rhel)
      if command -v dnf >/dev/null; then dnf install -y "$@"
      else                                yum install -y "$@"; fi
      ;;
    *) vd_die "unsupported OS family: $VD_OS_FAMILY" ;;
  esac
}

# vd_detect_ip — best-effort guess at the primary private IPv4.
vd_detect_ip() {
  local ip
  ip=$(ip route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}')
  if [[ -z "${ip:-}" ]]; then
    ip=$(hostname -I 2>/dev/null | awk '{print $1}')
  fi
  echo "$ip"
}

vd_install_docker() {
  if command -v docker >/dev/null 2>&1; then
    vd_ok "docker already installed: $(docker --version)"
    return
  fi
  vd_info "installing docker via get.docker.com"
  curl -fsSL https://get.docker.com | sh
  systemctl enable --now docker
  vd_ok "docker installed"
}

# vd_wait_url URL TIMEOUT_SECS
vd_wait_url() {
  local url="$1" timeout="${2:-120}"
  local i=0
  while (( i < timeout )); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2; ((i+=2))
  done
  return 1
}

# vd_random_base64 N
vd_random_base64() {
  head -c "$1" /dev/urandom | base64 | tr -d '\n=' | head -c "$1"
}
