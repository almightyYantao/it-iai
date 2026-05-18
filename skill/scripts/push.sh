#!/usr/bin/env bash
# deploy +push — scan → pack → create project (first run) → presigned upload → notify → SSE follow.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
source "$HERE/lib/common.sh"
source "$HERE/lib/scan.sh"
source "$HERE/lib/preflight.sh"

NO_FOLLOW=false
EXPLICIT_SLUG=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-follow) NO_FOLLOW=true; shift ;;
    --slug)      EXPLICIT_SLUG="$2"; shift 2 ;;
    *) vd_die "unknown flag: $1" ;;
  esac
done

# Make sure API URL + token are set BEFORE anything that touches the cluster —
# saves the user from packing a tarball just to fail at the upload step.
api_url=$(vd_ensure_api_url)
_=$(vd_token)  # triggers prompt-or-die, return value not needed yet
vd_info "API: $api_url"

vd_info "scanning $(pwd)"
manifest=$(vd_scan_manifest .)
echo "$manifest" | jq -c '.' >&2

# Preflight: cwd hygiene, Procfile autogen if needed. Advisory — never fails the push.
vd_preflight "$manifest"

# --- confirm detected sidecar needs ----------------------------------------
#
# scan.sh detects "this project imports a PG / Redis client" and sets
# needs.postgres / needs.redis on the manifest. The platform will use these
# to auto-provision a database and inject DATABASE_URL / REDIS_URL into the
# pod (no manual setup required from the user).
#
# But: detection is heuristic — a project might import psycopg in an offline
# data-migration script and not actually need a DB at runtime. Surface the
# detection and let the user opt out before we commit to provisioning.
# Non-TTY (CI, piped Claude tool call) accepts the detection as-is.
needs_pg=$(echo "$manifest" | jq -r '.needs.postgres // false')
needs_redis=$(echo "$manifest" | jq -r '.needs.redis // false')
needs_s3=$(echo "$manifest" | jq -r '.needs.s3 // false')

if [[ "$needs_pg" == "true" || "$needs_redis" == "true" || "$needs_s3" == "true" ]]; then
  vd_info "检测到依赖："
  [[ "$needs_pg"    == "true" ]] && vd_info "  PostgreSQL ✓（自动开独立 DB；注入 DATABASE_URL）"
  [[ "$needs_redis" == "true" ]] && vd_info "  Redis ✓（自动开 ACL 用户 + 独立 key 前缀；注入 REDIS_URL / REDIS_KEY_PREFIX）"
  [[ "$needs_s3"    == "true" ]] && vd_info "  S3 ✓（自动开独立 bucket + IAM user；注入 S3_ENDPOINT / S3_ACCESS_KEY_ID / S3_SECRET_ACCESS_KEY / S3_BUCKET 等）"

  if [[ -t 0 ]]; then
    printf '\033[36m?\033[0m 用平台自动开通这些服务？回车 = 是；输入 n = 跳过（你自己注入连接串） [Y/n]: ' >&2
    read -r ans || true
    if [[ "$ans" =~ ^[nN] ]]; then
      manifest=$(echo "$manifest" | jq '.needs.postgres = false | .needs.redis = false | .needs.s3 = false')
      vd_info "已跳过自动开通——记得自己在项目详情页填环境变量（或在 .vibedeploy.toml 里写 env）"
    fi
  fi
fi

# --- determine slug ---------------------------------------------------------

slug="$EXPLICIT_SLUG"
[[ -z "$slug" ]] && slug=$(vd_state_get slug || true)

if [[ -z "$slug" ]]; then
  # First push from this directory — create the project.
  #
  # Two-step prompt so users get a sensible default but can override:
  #   1. project name (display name; defaults to the scanner's guess, usually basename)
  #   2. slug (URL fragment; defaults to "" so the server picks a unique one)
  # Without a TTY we silently fall through to the manifest-derived name, which
  # mirrors the pre-prompt behaviour for CI / piped runs.
  default_name=$(echo "$manifest" | jq -r '.name')
  name="$default_name"
  desired_slug=""

  if [[ -t 0 ]]; then
    printf '\033[36m?\033[0m Project name [\033[2m%s\033[0m]: ' "$default_name" >&2
    read -r answer || true
    [[ -n "$answer" ]] && name="$answer"

    # Suggest a slug by lowercasing + hyphenating the name. The server will
    # still validate and reject anything unsafe; this is just to spare the
    # user from typing it twice when the default is what they want.
    suggested_slug=$(echo "$name" | tr '[:upper:]' '[:lower:]' \
      | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//' \
      | cut -c1-24)
    printf '\033[36m?\033[0m URL slug [\033[2m%s\033[0m] (empty for auto-suffix): ' "$suggested_slug" >&2
    read -r answer || true
    if [[ -n "$answer" ]]; then
      desired_slug="$answer"
    elif [[ -n "$suggested_slug" ]]; then
      desired_slug="$suggested_slug"
    fi
  fi

  # Strip empty-string keys from manifest before sending so the server can apply
  # its own defaults (avoids tripping over server-side nullable handling).
  body=$(mktemp)
  if [[ -n "$desired_slug" ]]; then
    jq -n --argjson m "$manifest" --arg name "$name" --arg slug "$desired_slug" \
      '{name: $name, slug: $slug, manifest: ($m | with_entries(select(.value != "" and .value != null)))}' > "$body"
  else
    jq -n --argjson m "$manifest" --arg name "$name" \
      '{name: $name, manifest: ($m | with_entries(select(.value != "" and .value != null)))}' > "$body"
  fi
  resp=$(vd_api POST /v1/projects "$body")
  rm -f "$body"
  slug=$(echo "$resp" | jq -r '.project.slug // empty')
  if [[ -z "$slug" ]]; then
    vd_die "create project succeeded but response had no slug; full body:
$resp"
  fi
  vd_state_set slug "$slug"
  vd_info "created project: $name → slug=$slug"
else
  vd_info "using existing slug=$slug"
fi

# --- pack source ------------------------------------------------------------

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT
tarball="$tmpdir/source.tar.zst"

vd_info "packing source"
# Two branches:
#   - inside a git repo: trust git, ls-files honours .gitignore for free
#   - otherwise: tar with a hand-curated exclude list + optional .deployignore
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  git ls-files -co --exclude-standard > "$tmpdir/files.txt"
  tar --files-from "$tmpdir/files.txt" -cf - 2>/dev/null | zstd -q -o "$tarball"
else
  # Default excludes: things that should never end up in an image, period.
  # .deployignore (gitignore syntax) is for project-specific additions.
  tar_excludes=(
    --exclude='./.git'
    --exclude='./.vibedeploy.json'
    # Python
    --exclude='./.venv' --exclude='./venv' --exclude='./env'
    --exclude='./__pycache__' --exclude='*.pyc'
    --exclude='./.pytest_cache' --exclude='./.mypy_cache' --exclude='./.tox'
    # Node
    --exclude='./node_modules'
    --exclude='./.next' --exclude='./.nuxt'
    # General build output
    --exclude='./dist' --exclude='./build' --exclude='./target'
    # Editor / OS
    --exclude='./.idea' --exclude='./.vscode'
    --exclude='./.DS_Store' --exclude='._*'
  )
  if [[ -f .deployignore ]]; then
    tar_excludes+=(--exclude-from=.deployignore)
    vd_info "honouring .deployignore"
  fi
  tar "${tar_excludes[@]}" -cf - . | zstd -q -o "$tarball"
fi
size=$(wc -c < "$tarball" | tr -d ' ')
human=$(awk -v b="$size" 'BEGIN{ split("B KB MB GB", u); i=1; while (b>=1024 && i<4){ b=b/1024; i++ } printf "%.1f %s", b, u[i] }')
vd_info "packed source ($human)"

if [[ "$size" -gt $((50 * 1024 * 1024)) ]]; then
  vd_warn "tarball > 50MB; consider .deployignore"
fi

# --- create deployment ------------------------------------------------------

vd_info "creating deployment"
body=$(mktemp)
jq -n --argjson m "$manifest" \
  '{manifest: ($m | with_entries(select(.value != "" and .value != null)))}' > "$body"
dep=$(vd_api POST "/v1/projects/$slug/deployments" "$body")
rm -f "$body"

dep_id=$(echo "$dep"     | jq -r '.deployment_id // empty')
upload_url=$(echo "$dep" | jq -r '.upload_url    // empty')
events_url=$(echo "$dep" | jq -r '.events_url    // empty')

# Validate everything we need before any side-effecting curl — otherwise a
# missing field shows up downstream as "curl: option : blank argument" or
# similar, which masks the real cause.
[[ -z "$dep_id"     ]] && vd_die "deployment response missing deployment_id; body: $dep"
[[ -z "$upload_url" ]] && vd_die "deployment response missing upload_url; body: $dep"
[[ -z "$events_url" ]] && vd_die "deployment response missing events_url; body: $dep"

vd_info "uploading source to object storage"
curl -fsS -X PUT --data-binary "@$tarball" -H 'Content-Type: application/zstd' "$upload_url" >/dev/null
vd_info "uploaded ($human)"

# --- notify control plane ---------------------------------------------------

vd_api POST "/v1/projects/$slug/deployments/$dep_id/uploaded" >/dev/null
vd_info "queued for build (dep=$dep_id)"

# --- follow events ----------------------------------------------------------

if $NO_FOLLOW; then
  echo "$events_url"
  exit 0
fi

tok=$(vd_token)
# stream SSE; print phase/message lines
curl -N -sS \
  -H "Authorization: Bearer $tok" \
  -H "Accept: text/event-stream" \
  "$events_url" \
| awk -v RS= -v ORS='\n' '
    /^id: /     { next }
    /^event: end/ { print "[done]"; exit }
    /^data: /   {
      sub(/^data: /, "")
      # data line is a JSON object; pluck phase + message with sed-ish tricks
      msg=$0
      gsub(/.*"phase":"/,"",$0); phase=$0; gsub(/".*/,"",phase)
      gsub(/.*"message":"/,"",msg); gsub(/".*/,"",msg)
      printf "[%s] %s\n", phase, msg
    }
'

# --- final status -----------------------------------------------------------
#
# Look at THIS deployment specifically, not the project — the project's status
# could be stale or reflect a different deployment. failure_reason lives on the
# deployment row and is the most useful single piece of info on a failure.

dep_final=$(vd_api GET "/v1/projects/$slug/deployments/$dep_id" || true)
status=$(echo "$dep_final" | jq -r '.deployment.status // empty')
reason=$(echo "$dep_final" | jq -r '.deployment.failure_reason // empty')

case "$status" in
  running)
    proj=$(vd_api GET "/v1/projects/$slug")
    url=$(echo "$proj" | jq -r '.project.url')
    printf '\n\033[32m🚀 %s\033[0m\n' "$url"
    ;;
  failed)
    vd_err "deployment failed"
    [[ -n "$reason" ]] && vd_err "  reason: $reason"
    vd_warn "see full build log:  deploy +logs --dep $dep_id"
    vd_warn "or in Claude Code:   \"deploy +logs\""
    exit 1
    ;;
  superseded)
    vd_warn "this deployment was superseded by a newer one — yours is no longer the latest"
    exit 1
    ;;
  pending|queued|building|pushing|deploying)
    vd_warn "stream closed but deployment still in flight (status=$status)"
    vd_warn "poll status with:  deploy +status"
    exit 1
    ;;
  "")
    vd_err "could not read deployment status; try: deploy +status $slug"
    exit 1
    ;;
  *)
    vd_err "unexpected final status: $status"
    [[ -n "$reason" ]] && vd_err "  reason: $reason"
    exit 1
    ;;
esac
