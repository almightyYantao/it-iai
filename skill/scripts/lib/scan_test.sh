#!/usr/bin/env bash
# Smoke tests for vd_scan_manifest. Not a full bats suite — just enough to catch
# the regressions we've had (TOML $3-vs-$2 bug, framework-aware port defaults).
#
# Run: bash skill/scripts/lib/scan_test.sh

set -o pipefail   # not -e (keep going across cases), not -u (we use defaulted params)

HERE="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=scan.sh
source "$HERE/scan.sh"

fail=0
pass=0

# assert KEY EXPECTED ACTUAL CASE
assert() {
  local key="$1" want="$2" got="$3" case="$4"
  if [[ "$got" == "$want" ]]; then
    printf '  \033[32m✓\033[0m %-22s · %s = %q\n' "$case" "$key" "$got"
    pass=$((pass+1))
  else
    printf '  \033[31m✗\033[0m %-22s · %s want=%q got=%q\n' "$case" "$key" "$want" "$got"
    fail=$((fail+1))
  fi
}

# scenario CASE [PORT] [LANG] [NAME] [START_SUBSTR]   then a setup block via stdin
# Runs the setup inside a tmpdir whose basename = CASE (so vd_scan_manifest's
# auto-name fallback is predictable).
#
# START_SUBSTR (optional): a substring that must appear inside .start. Pass
# empty string to skip that check.
scenario() {
  local case="$1"
  local want_port="${2-}" want_lang="${3-}" want_name="${4-}" want_start_substr="${5-}"
  [[ -z "$want_name" ]] && want_name="$case"
  local root tmp
  root=$(mktemp -d)
  tmp="$root/$case"
  mkdir -p "$tmp"
  # Read setup script from caller's stdin and run inside $tmp.
  ( cd "$tmp" && bash -s ) || { echo "setup failed for $case" >&2; return; }

  local manifest start
  manifest=$(cd "$tmp" && vd_scan_manifest . 2>/dev/null)
  [[ -n "$want_name" ]] && assert name "$want_name" "$(jq -r .name      <<<"$manifest")" "$case"
  [[ -n "$want_port" ]] && assert port "$want_port" "$(jq -r .port      <<<"$manifest")" "$case"
  [[ -n "$want_lang" ]] && assert lang "$want_lang" "$(jq -r .language  <<<"$manifest")" "$case"
  if [[ -n "$want_start_substr" ]]; then
    start=$(jq -r .start <<<"$manifest")
    if [[ "$start" == *"$want_start_substr"* ]]; then
      printf '  \033[32m✓\033[0m %-22s · start contains %q\n' "$case" "$want_start_substr"
      pass=$((pass+1))
    else
      printf '  \033[31m✗\033[0m %-22s · start want substring %q got %q\n' "$case" "$want_start_substr" "$start"
      fail=$((fail+1))
    fi
  fi
  rm -rf "$root"
}

echo '== TOML parsing (the original $3-vs-$2 bug) =='
scenario toml-explicit  5000  node  sales-dashboard <<'EOF'
cat > .vibedeploy.toml <<'TOML'
name = "sales-dashboard"
port = 5000
start = "node server.js"
TOML
cat > package.json <<'JSON'
{"name":"x","scripts":{"start":"node server.js"}}
JSON
EOF

scenario toml-port-only 9090  ""    "" <<'EOF'
cat > .vibedeploy.toml <<'TOML'
port = 9090
TOML
EOF

echo
echo "== Framework-aware default ports =="
scenario py-flask    5000  python  py-flask    "flask run" <<'EOF'
echo "flask==2.3" > requirements.txt
EOF

scenario py-fastapi  8000  python  py-fastapi  "uvicorn main:app" <<'EOF'
cat > requirements.txt <<'REQ'
fastapi
uvicorn[standard]
REQ
EOF

scenario py-django   8000  python  py-django   "manage.py runserver" <<'EOF'
echo "django>=4.0" > requirements.txt
EOF

scenario node-app    3000  node <<'EOF'
echo '{"scripts":{"start":"node a.js"}}' > package.json
EOF

scenario go-app      8080  go <<'EOF'
echo "module example.com/x" > go.mod
EOF

echo
echo "passed: $pass  failed: $fail"
[[ "$fail" -eq 0 ]]
