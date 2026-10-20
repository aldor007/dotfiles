#!/usr/bin/env bash
# validate.sh — validate the Neovim config end-to-end.
#
# Checks:
#   1. Lua syntax of every config/plugin file (nvim -l; no luac dependency)
#   2. Clean headless startup
#   3. Plugin dependency check: every spec's repo must be in lazy-lock.json
#   4. Per-plugin smoke test (scripts/smoke_test.lua — offline, stubbed gcloud)
#   5. checkhealth capture — REAL ERROR lines (excluding known-optional tooling)
#      fail the run
#   6. Startup time measurement
#
# Safety: by default this does NOT touch your plugin installs (no Lazy sync,
# no updates). For a fully hermetic run that installs plugins into a
# throwaway XDG dir (needs network on first run) use --isolated (CI-style).
# Run `--sync` deliberately to install/update plugins first.
#
# Works from ANY working directory: the nvim dir is resolved from the
# script's own location, never from $PWD.
#
# Usage:
#   validate.sh [--isolated] [--sync] [-h|--help]
set -u

NVIM_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$NVIM_DIR" || { echo "validate: cannot cd to $NVIM_DIR" >&2; exit 1; }

ISOLATED=0
SYNC=0
for arg in "$@"; do
  case "$arg" in
    --isolated) ISOLATED=1 ;;
    --sync) SYNC=1 ;;
    -h|--help) sed -n '1,30p' "$0"; exit 0 ;;
    *) echo "validate: unknown arg: $arg" >&2; exit 2 ;;
  esac
done

# timeout fallback (macOS ships no `timeout`; gtimeout from coreutils)
if ! command -v timeout >/dev/null 2>&1; then
  if command -v gtimeout >/dev/null 2>&1; then
    timeout() { gtimeout "$@"; }
  else
    timeout() { "$@"; } # best effort: no enforcement
  fi
fi

fail=0
step() { echo; echo "==> $1"; }

ENV_SETUP=()
ISO_ROOT=""
if [ "$ISOLATED" = 1 ]; then
  ISO_ROOT=$(mktemp -d)
  trap 'rm -rf "$ISO_ROOT"' EXIT
  mkdir -p "$ISO_ROOT/config" "$ISO_ROOT/data" "$ISO_ROOT/cache"
  # NOTE: link the resolved nvim dir — $PWD is intentionally NOT used
  ln -s "$NVIM_DIR" "$ISO_ROOT/config/nvim"
  ENV_SETUP=(env XDG_CONFIG_HOME="$ISO_ROOT/config" XDG_DATA_HOME="$ISO_ROOT/data" XDG_CACHE_HOME="$ISO_ROOT/cache")
  echo "(isolated mode — plugins install into $ISO_ROOT/data; first run downloads them)"
fi
NV=("${ENV_SETUP[@]}" nvim)

step "1/6 Lua syntax check"
for f in init.lua lua/config/*.lua lua/plugins/*.lua scripts/smoke_test.lua; do
  if "${ENV_SETUP[@]}" nvim -l /dev/stdin <<< "assert(loadfile('$NVIM_DIR/$f'))" >/dev/null 2>&1; then
    echo "OK    $f"
  else
    echo "FAIL  $f"; fail=1
  fi
done

step "2/6 Clean headless startup"
if "${NV[@]}" --headless "+qa" >/dev/null 2>&1; then
  echo "OK    ${NV[*]} --headless +qa"
else
  echo "FAIL  startup errors:"; "${NV[@]}" --headless "+qa" 2>&1 | head -20; fail=1
fi

step "3/6 Plugin dependency check (specs vs nvim/lazy-lock.json)"
LOCK="$NVIM_DIR/lazy-lock.json"
missing=0
for spec in "$NVIM_DIR"/lua/plugins/*.lua; do
  [ -e "$spec" ] || continue
  repo=$(grep -oE '\{\s*"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+"' "$spec" | head -1 | sed 's/[{" ]//g')
  [ -z "$repo" ] && continue
  base=${repo#*/}
  if ! grep -q "\"$base\"" "$LOCK" 2>/dev/null; then
    echo "MISS  $(basename "$spec") -> $repo not in lazy-lock.json (run validate.sh --sync once)"
    missing=$((missing+1))
  fi
done
if [ "$missing" -eq 0 ]; then
  echo "OK    all spec repos present in lazy-lock.json"
else
  echo "      ($missing spec(s) not locked — install order: --sync, then re-run)"
  fail=1
fi

if [ "$SYNC" = 1 ]; then
  step "3b/6 Lazy sync (installs/updates plugins — mutating!)"
  if timeout 600 "${NV[@]}" --headless "+Lazy! sync" +qa >/dev/null 2>&1; then
    echo "OK    Lazy sync"
  else
    echo "FAIL  Lazy sync"; fail=1
  fi
fi

step "4/6 Plugin smoke test"
if timeout 300 "${NV[@]}" --headless "+luafile $NVIM_DIR/scripts/smoke_test.lua" 2>&1; then
  :
else
  fail=1
fi

step "5/6 checkhealth (errors only)"
health_file="${ISO_ROOT:-/tmp}/nvim-health.txt"
if timeout 120 "${NV[@]}" --headless "+checkhealth" "+w! $health_file" +qa >/dev/null 2>&1; then
  # Real errors fail validation. Known-benign optional tooling is excluded:
  # luarocks/lua 5.1 (lazy-rocks; no installed plugin uses rocks),
  # composer/php/jul javac/julia (language toolchains we don't use).
  n_err=$(grep -E "^\s*ERROR" "$health_file" \
    | grep -vcE "luarocks|lua.*5\.1|composer|^.*php[^:]*:|javac|julia|hererocks" || true)
  echo "health report: $health_file  (ERROR lines: $n_err — excluding optional tooling)"
  if [ "$n_err" -gt 0 ]; then
    echo "FAIL  checkhealth reports real errors:"; grep -E "^\s*ERROR" "$health_file" \
      | grep -vE "luarocks|lua.*5\.1|composer|^.*php[^:]*:|javac|julia|hererocks" | head -10
    fail=1
  fi
else
  echo "FAIL  checkhealth run"; fail=1
fi

step "6/6 Startup time"
startup_file="${ISO_ROOT:-/tmp}/nvim-startup.txt"
"${NV[@]}" --headless --startuptime "$startup_file" "+qa" >/dev/null 2>&1
grep "NVIM STARTED" "$startup_file" | tail -1

echo
if [ "$fail" -eq 0 ]; then
  echo "ALL CHECKS PASSED"
else
  echo "VALIDATION FAILED"
  exit 1
fi