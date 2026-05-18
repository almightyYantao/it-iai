#!/usr/bin/env bash
# Deterministic project scan. Outputs a manifest JSON to stdout.
# Rules are intentionally dumb / explicit — never guess via LLM here.

set -euo pipefail

# Default port chosen when no .vibedeploy.toml + no override applies.
# Reassigned by language detection below based on framework conventions.
_VD_DEFAULT_PORT=3000

# vd_scan_manifest [DIR]
vd_scan_manifest() {
  local dir="${1:-.}"
  cd "$dir"

  local name port build="" start="" lang="" needs_pg=false needs_redis=false
  local port_explicit=false

  # 1) explicit override from .vibedeploy.toml
  if [[ -f .vibedeploy.toml ]]; then
    # Minimal TOML extraction — only the leaf keys we care about.
    # For `key = "value"` (-F'[" =]+') the fields are: $1=key, $2=value, $3=""
    # For `key = 5000`    (-F'[ =]+')  the fields are: $1=key, $2=5000
    # So $2 is always the value column — earlier code used $3 by mistake.
    name=$(awk  -F'[" =]+' '/^name *=/{print $2; exit}' .vibedeploy.toml || true)
    local toml_port
    toml_port=$(awk -F'[ =]+'  '/^port *=/{print $2; exit}' .vibedeploy.toml || true)
    build=$(awk -F'"'         '/^build *=/{print $2; exit}' .vibedeploy.toml || true)
    start=$(awk -F'"'         '/^start *=/{print $2; exit}' .vibedeploy.toml || true)
    grep -qE '^postgres *= *true' .vibedeploy.toml && needs_pg=true
    grep -qE '^redis *= *true'    .vibedeploy.toml && needs_redis=true
    if [[ -n "$toml_port" ]]; then
      port=$toml_port
      port_explicit=true
    fi
  fi

  if [[ -z "${name:-}" ]]; then
    name=$(basename "$PWD")
  fi

  # 2) detect language (may also adjust default port per framework convention)
  if [[ -f package.json ]]; then
    lang="node"
    [[ -z "$start" ]] && start=$(jq -r '.scripts.start // empty' package.json 2>/dev/null || true)
    [[ -z "$build" ]] && build=$(jq -r '.scripts.build // empty' package.json 2>/dev/null || true)
    grep -qE '"(pg|prisma|sequelize|typeorm|knex|drizzle-orm)"' package.json && needs_pg=true
    grep -qE '"(ioredis|redis)"' package.json && needs_redis=true
    : "${port:=3000}"
  elif [[ -f requirements.txt || -f pyproject.toml ]]; then
    lang="python"

    # Detect once, use everywhere. Match at start-of-line so we don't false-positive
    # on transitive deps like 'flask-cors' or comments mentioning flask.
    local _has_flask=false _has_fastapi=false _has_django=false
    grep -qiE '^[[:space:]]*flask([[:space:]<>=~,!]|$)'         requirements.txt pyproject.toml 2>/dev/null && _has_flask=true
    grep -qiE '^[[:space:]]*(fastapi|uvicorn)([[:space:]<>=~,!]|$)' requirements.txt pyproject.toml 2>/dev/null && _has_fastapi=true
    grep -qiE '^[[:space:]]*django([[:space:]<>=~,!]|$)'        requirements.txt pyproject.toml 2>/dev/null && _has_django=true

    grep -qiE 'psycopg|sqlalchemy|asyncpg|django' requirements.txt pyproject.toml 2>/dev/null && needs_pg=true
    grep -qiE 'redis'                              requirements.txt pyproject.toml 2>/dev/null && needs_redis=true

    # Framework-aware default port. Flask=5000, FastAPI/Django=8000.
    if [[ -z "${port:-}" ]]; then
      if   $_has_flask;                     then port=5000
      elif $_has_fastapi || $_has_django;   then port=8000
      else                                       port=8000
      fi
    fi

    # Framework-aware default start command. Use single quotes so $PORT stays
    # literal — it'll be expanded inside the container at runtime by the shell
    # that nixpacks generates around the start command.
    if [[ -z "$start" ]]; then
      if   $_has_fastapi; then start='uvicorn main:app --host 0.0.0.0 --port $PORT'
      elif $_has_flask;   then start='flask run --host=0.0.0.0 --port=$PORT'
      elif $_has_django;  then start='python manage.py runserver 0.0.0.0:$PORT'
      fi
    fi
  elif [[ -f go.mod ]]; then
    lang="go"
    : "${port:=8080}"
  elif [[ -f Cargo.toml ]]; then
    lang="rust"
    : "${port:=8080}"
  else
    : "${port:=$_VD_DEFAULT_PORT}"
  fi

  # Warn when we fell back to a default (helps users notice when they should pin it).
  if [[ "$port_explicit" != "true" ]]; then
    printf '\033[33m!\033[0m using default port %s — set `port = N` in .vibedeploy.toml to pin\n' "$port" >&2
  fi

  # 3) emit manifest
  jq -n \
    --arg name  "$name" \
    --arg lang  "$lang" \
    --arg build "$build" \
    --arg start "$start" \
    --argjson port "$port" \
    --argjson postgres "$needs_pg" \
    --argjson redis    "$needs_redis" '
  {
    name: $name,
    language: $lang,
    port: $port,
    build: ($build // ""),
    start: ($start // ""),
    needs: { postgres: $postgres, redis: $redis }
  }'
}
