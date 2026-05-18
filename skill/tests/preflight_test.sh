#!/usr/bin/env bash
# Smoke tests for vd_preflight. Each case sets up a tmp project tree,
# invokes vd_preflight with a manifest, and asserts side effects.
#
# Run: bash skill/tests/preflight_test.sh

set -o pipefail  # NOT -e — we keep going across failing assertions

HERE="$(cd "$(dirname "$0")" && pwd)"

# Stub the logging functions preflight.sh expects from common.sh. Keeping the
# test self-contained avoids dragging in jq/tar/zstd dependency checks done by
# common.sh's top-level vd_need calls.
vd_warn() { printf 'WARN: %s\n' "$*" >&2; }
vd_info() { printf 'INFO: %s\n' "$*" >&2; }
vd_err()  { printf 'ERR:  %s\n' "$*" >&2; }
vd_die()  { vd_err "$*"; exit 1; }

# shellcheck source=../scripts/lib/preflight.sh
source "$HERE/../scripts/lib/preflight.sh"

pass=0
fail=0

assert() {
  local case="$1" cond="$2" detail="$3"
  if eval "$cond"; then
    printf '  \033[32m✓\033[0m %-32s %s\n' "$case" "$detail"
    pass=$((pass+1))
  else
    printf '  \033[31m✗\033[0m %-32s %s\n' "$case" "$detail"
    fail=$((fail+1))
  fi
}

# new_tmp NAME → echoes a fresh empty dir whose basename = NAME.
new_tmp() {
  local root tmp
  root=$(mktemp -d)
  tmp="$root/$1"
  mkdir -p "$tmp"
  echo "$tmp"
}

# ============================================================================
echo '== bloat-dir warning =='
# ============================================================================

tmp=$(new_tmp bloat-case)
mkdir "$tmp/node_modules" "$tmp/__pycache__" "$tmp/.venv"
output=$(cd "$tmp" && vd_preflight '{}' 2>&1)
assert "warn-on-node_modules" '[[ "$output" == *node_modules* ]]'  "stderr mentions node_modules"
assert "warn-on-pycache"      '[[ "$output" == *__pycache__* ]]'   "stderr mentions __pycache__"
assert "warn-on-venv"         '[[ "$output" == *.venv* ]]'         "stderr mentions .venv"
rm -rf "$(dirname "$tmp")"

# ============================================================================
echo
echo '== Python Procfile autogen =='
# ============================================================================

tmp=$(new_tmp py-noframework)
echo "psycopg2-binary" > "$tmp/requirements.txt"
echo "print('hi')" > "$tmp/app.py"
output=$(cd "$tmp" && vd_preflight '{"language":"python","start":""}' 2>&1)
assert "procfile-created"        '[[ -f "$tmp/Procfile" ]]'                                                "Procfile exists"
assert "procfile-content-app.py" '[[ "$(cat "$tmp/Procfile")" == "web: python app.py" ]]'                  "content = 'web: python app.py'"
assert "procfile-warn-emitted"   '[[ "$output" == *"generated Procfile"* ]]'                               "warn line printed"
rm -rf "$(dirname "$tmp")"

# Prefers app.py over main.py when both exist
tmp=$(new_tmp py-prefer-app)
echo "" > "$tmp/app.py"
echo "" > "$tmp/main.py"
(cd "$tmp" && vd_preflight '{"language":"python","start":""}' 2>/dev/null)
assert "procfile-prefers-app.py" '[[ "$(cat "$tmp/Procfile")" == "web: python app.py" ]]'  "picks app.py first"
rm -rf "$(dirname "$tmp")"

# Falls back to main.py if no app.py
tmp=$(new_tmp py-fallback-main)
echo "" > "$tmp/main.py"
(cd "$tmp" && vd_preflight '{"language":"python","start":""}' 2>/dev/null)
assert "procfile-fallback-main"  '[[ "$(cat "$tmp/Procfile")" == "web: python main.py" ]]' "falls back to main.py"
rm -rf "$(dirname "$tmp")"

# ============================================================================
echo
echo '== Procfile not overwritten when one exists =='
# ============================================================================

tmp=$(new_tmp py-existing-procfile)
echo "fastapi" > "$tmp/requirements.txt"
echo "web: gunicorn x.wsgi" > "$tmp/Procfile"
echo "" > "$tmp/app.py"
(cd "$tmp" && vd_preflight '{"language":"python","start":""}' 2>/dev/null)
assert "procfile-untouched" '[[ "$(cat "$tmp/Procfile")" == "web: gunicorn x.wsgi" ]]' "user Procfile preserved"
rm -rf "$(dirname "$tmp")"

# ============================================================================
echo
echo '== No Procfile when manifest.start is set =='
# ============================================================================

tmp=$(new_tmp py-with-start)
echo "fastapi" > "$tmp/requirements.txt"
echo "" > "$tmp/app.py"
(cd "$tmp" && vd_preflight '{"language":"python","start":"uvicorn main:app --port $PORT"}' 2>/dev/null)
assert "no-procfile-when-start" '[[ ! -f "$tmp/Procfile" ]]' "Procfile NOT created"
rm -rf "$(dirname "$tmp")"

# ============================================================================
echo
echo '== No Procfile when Dockerfile present =='
# ============================================================================

tmp=$(new_tmp py-dockerfile)
echo "" > "$tmp/Dockerfile"
echo "" > "$tmp/app.py"
(cd "$tmp" && vd_preflight '{"language":"python","start":""}' 2>/dev/null)
assert "no-procfile-when-docker" '[[ ! -f "$tmp/Procfile" ]]' "Dockerfile wins, skip Procfile gen"
rm -rf "$(dirname "$tmp")"

# ============================================================================
echo
echo '== Non-Python project: no Procfile written =='
# ============================================================================

tmp=$(new_tmp node-app)
echo '{"scripts":{"start":"node x.js"}}' > "$tmp/package.json"
(cd "$tmp" && vd_preflight '{"language":"node","start":"node x.js"}' 2>/dev/null)
assert "no-procfile-for-node" '[[ ! -f "$tmp/Procfile" ]]' "Procfile only for Python"
rm -rf "$(dirname "$tmp")"

# ============================================================================
echo
echo '== Darwin sets COPYFILE_DISABLE =='
# ============================================================================

if [[ "$(uname)" == "Darwin" ]]; then
  unset COPYFILE_DISABLE
  tmp=$(new_tmp darwin-case)
  (cd "$tmp" && vd_preflight '{}' >/dev/null 2>&1)
  # vd_preflight runs in the calling shell (sourced function), so it should set
  # COPYFILE_DISABLE in our process. We exported it, so it persists past the
  # subshell-less call.
  vd_preflight '{}' >/dev/null 2>&1   # call directly so the export reaches us
  assert "copyfile-disable-darwin" '[[ "$COPYFILE_DISABLE" == "1" ]]' "COPYFILE_DISABLE=1"
  rm -rf "$(dirname "$tmp")"
else
  echo "  (skipped: not on Darwin)"
fi

# ============================================================================
echo
echo "passed: $pass  failed: $fail"
[[ "$fail" -eq 0 ]]
