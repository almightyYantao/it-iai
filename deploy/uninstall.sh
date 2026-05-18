#!/usr/bin/env bash
# Tear down VibeDeploy from a node. Run on platform OR worker.
# Asks for confirmation. Destroys docker volumes (= all data) on platform nodes.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$HERE/.." && pwd)"
# shellcheck source=lib/_common.sh
source "$HERE/lib/_common.sh"

vd_require_root

confirm() {
  local prompt="$1"
  echo -n "$prompt [y/N]: " >&2
  read -r ans
  [[ "$ans" =~ ^[Yy]$ ]]
}

vd_warn "This will:"
vd_warn "  - stop and remove the docker-compose stack (incl. PG / MinIO volumes)"
vd_warn "  - run k3s-uninstall.sh / k3s-agent-uninstall.sh if installed"
vd_warn "  - leave /etc/vibedeploy and the repo files in place"
confirm "Continue?" || { vd_warn "aborted"; exit 0; }

if [[ -f "$REPO_ROOT/docker-compose.yml" ]]; then
  vd_info "stopping docker-compose stack"
  (cd "$REPO_ROOT" && docker compose down -v --remove-orphans) || true
fi

if [[ -x /usr/local/bin/k3s-uninstall.sh ]]; then
  vd_info "removing K3s server"
  /usr/local/bin/k3s-uninstall.sh || true
fi
if [[ -x /usr/local/bin/k3s-agent-uninstall.sh ]]; then
  vd_info "removing K3s agent"
  /usr/local/bin/k3s-agent-uninstall.sh || true
fi

vd_info "removing /etc/rancher/k3s and /root/.kube/config (if present)"
rm -rf /etc/rancher/k3s /root/.kube/config

vd_ok "uninstall complete. Docker itself was left installed."
