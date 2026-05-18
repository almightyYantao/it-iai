#!/usr/bin/env bash
# vd_preflight: cwd-side hygiene & "make sure this thing can actually start" hooks
# run before packaging. Pure library — doesn't depend on the API, the token,
# or the network. Sourced by push.sh; also exercised by tests/preflight_test.sh.

# vd_preflight MANIFEST_JSON
#
# Side effects (intentional, all visible to the user):
#   - On macOS, exports COPYFILE_DISABLE=1 so tar doesn't include ._* AppleDouble files.
#   - Warns about directories that almost always shouldn't be uploaded (.venv, node_modules, ...)
#     when they actually exist in cwd.
#   - When the manifest says language=python AND there's no Dockerfile / Procfile / start
#     command, picks the first matching entry point (app.py / main.py / server.py / wsgi.py)
#     and writes `web: python <entry>` to a Procfile so nixpacks can find a start command.
#
# Returns 0 always. Failures are reported via vd_warn — preflight is advisory, not gating.
vd_preflight() {
  local manifest="${1-{\}}"

  # 1) Darwin tar wart: AppleDouble files (._foo) leak resource forks into tarballs.
  #    They look like UTF-8 garbage to downstream tools (e.g. nixpacks reads .py files).
  if [[ "$(uname)" == "Darwin" ]]; then
    export COPYFILE_DISABLE=1
  fi

  # 2) Bloat / cache directories. These are the usual suspects that bloat the upload,
  #    bake stale state into the image, or carry platform-specific binaries that won't
  #    run inside Linux containers (venv shebangs, native node_modules, ...).
  local -a bloat=()
  local d
  for d in .venv venv env __pycache__ node_modules .next dist build .pytest_cache .mypy_cache target .tox; do
    [[ -d "$d" ]] && bloat+=("$d")
  done
  if (( ${#bloat[@]} > 0 )); then
    vd_warn "these directories will be packaged unless excluded: ${bloat[*]}"
    vd_warn "  for git repos: add them to .gitignore"
    vd_warn "  otherwise:     add a .deployignore (gitignore syntax)"
  fi

  # 3) Python projects need *some* way to start. nixpacks can usually figure it out
  #    from a `[tool.poetry.scripts]` entry, framework convention, or a Procfile.
  #    If none of those exist, a Procfile pointing at the obvious entry file makes
  #    the difference between a successful build and an opaque nixpacks error.
  local lang start
  lang=$(printf '%s' "$manifest" | jq -r '.language // empty' 2>/dev/null || true)
  start=$(printf '%s' "$manifest" | jq -r '.start    // empty' 2>/dev/null || true)

  if [[ "$lang" == "python" && -z "$start" && ! -f Dockerfile && ! -f Procfile ]]; then
    local entry=""
    local f
    for f in app.py main.py server.py wsgi.py asgi.py; do
      [[ -f "$f" ]] && { entry="$f"; break; }
    done
    if [[ -n "$entry" ]]; then
      printf 'web: python %s\n' "$entry" > Procfile
      vd_warn "no Dockerfile / Procfile and manifest.start is empty"
      vd_warn "  generated Procfile: 'web: python ${entry}'"
      vd_warn "  edit Procfile, or set 'start = \"...\"' in .vibedeploy.toml to override"
    else
      vd_warn "Python project but no Dockerfile / Procfile / app.py / main.py / server.py — build will likely fail"
      vd_warn "  add a Procfile  (e.g. 'web: python myapp.py')"
      vd_warn "  or .vibedeploy.toml with start = \"...\""
    fi
  fi
}
