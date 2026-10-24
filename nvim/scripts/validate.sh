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
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/nvim-validate.XXXXXX")
trap 'rm -rf -- "$TMP_DIR"' EXIT
ISO_ROOT=""
XDG_STATE_ROOT=""
if [ "$ISOLATED" = 1 ]; then
  ISO_ROOT="$TMP_DIR/isolated"
  mkdir -p "$ISO_ROOT/config" "$ISO_ROOT/data" "$ISO_ROOT/cache" "$ISO_ROOT/state/nvim"
  XDG_STATE_ROOT="$ISO_ROOT/state"
  # NOTE: link the resolved nvim dir — $PWD is intentionally NOT used
  ln -s "$NVIM_DIR" "$ISO_ROOT/config/nvim"
  ENV_SETUP=(env XDG_CONFIG_HOME="$ISO_ROOT/config" XDG_DATA_HOME="$ISO_ROOT/data" XDG_STATE_HOME="$ISO_ROOT/state" XDG_CACHE_HOME="$ISO_ROOT/cache")
  echo "(isolated mode — plugins install into $ISO_ROOT/data; first run downloads them)"
elif [ "$SYNC" = 1 ]; then
  XDG_ROOT="$TMP_DIR/xdg"
  mkdir -p "$XDG_ROOT/config" "$XDG_ROOT/data" "$XDG_ROOT/cache" "$XDG_ROOT/state/nvim"
  ln -s "$NVIM_DIR" "$XDG_ROOT/config/nvim"
  XDG_STATE_ROOT="$XDG_ROOT/state"
  ENV_SETUP=(env XDG_CONFIG_HOME="$XDG_ROOT/config" XDG_DATA_HOME="$XDG_ROOT/data" XDG_STATE_HOME="$XDG_ROOT/state" XDG_CACHE_HOME="$XDG_ROOT/cache")
  echo "(sync mode — disposable XDG state under $XDG_ROOT)"
fi
NV=("${ENV_SETUP[@]}" nvim)

# gcloud stub: config/db.lua seeds an IAM token at startup (VeryLazy) and
# smoke_test also calls seed(); every nvim invocation below must hit this
# stub, never a real gcloud. Markers prove the stub served the requests.
STUB_DIR="$TMP_DIR/gcloud-stub-validate"
mkdir -p "$STUB_DIR"
rm -f "$STUB_DIR/markers" "$STUB_DIR/called"
cat > "$STUB_DIR/gcloud" <<GSEOF
#!/bin/sh
echo called >> "$STUB_DIR/markers"
case "\$*" in
  "auth print-access-token") echo "fake-token-validate-000";;
  "auth application-default print-access-token") echo "fake-adc";;
esac
GSEOF
chmod +x "$STUB_DIR/gcloud"
export PATH="$STUB_DIR:$PATH"

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
if [ ! -f "$LOCK" ]; then
  echo "FAIL  lazy-lock.json missing at $LOCK"; fail=1
else
  if ! ( jq -e . "$LOCK" >/dev/null 2>&1 \
      || python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$LOCK" >/dev/null 2>&1 ); then
    echo "FAIL  $LOCK is not valid JSON"; fail=1
  else
    echo "OK    lazy-lock.json present + valid JSON"
  fi
fi
missing=0
specs=0
for spec in "$NVIM_DIR"/lua/plugins/*.lua; do
  [ -e "$spec" ] || continue
  specs=$((specs+1))
  repo=$(grep -oE '\{\s*"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+"' "$spec" | head -1 | sed 's/[{" ]//g')
  [ -z "$repo" ] && continue
  base=${repo#*/}
  if ! grep -q "\"$base\"" "$LOCK" 2>/dev/null; then
    echo "MISS  $(basename "$spec") -> $repo not in lazy-lock.json (run validate.sh --sync once)"
    missing=$((missing+1))
  fi
done
if [ "$specs" -eq 0 ]; then
  echo "FAIL  no plugin specifications found under $NVIM_DIR/lua/plugins"
  fail=1
elif [ "$missing" -eq 0 ]; then
  echo "OK    all $specs spec repos present in lazy-lock.json"
else
  echo "      ($missing spec(s) not locked — install order: --sync, then re-run)"
  fail=1
fi

if [ "$SYNC" = 1 ]; then
  step "3b/6 Lazy sync (installs/updates plugins — mutating!)"
  if timeout 600 "${NV[@]}" --headless "+Lazy! sync" +qa >/dev/null 2>&1; then
    echo "OK    Lazy sync"
    # later nvim invocations (smoke tests) must NOT re-run the treesitter
    # install — it races with the parsers the sync just installed
    export NVIM_TS_INSTALL_SKIP=1
    # pre-create the hardtime state log (hardtime opens it at load; a
    # missing file makes it abort with "Unable to open")
    hardtime_state_root="${XDG_STATE_ROOT:-${XDG_STATE_HOME:-$HOME/.local/state}}"
    mkdir -p "$hardtime_state_root/nvim"
    : > "$hardtime_state_root/nvim/hardtime.nvim.log"
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

step "4b/6 Cold-start smoke test (audit §3 testing gap)"
# SMOKE_COLD=1 verifies that plugins load via their natural triggers
# (events, keys, commands) — not the force-load path. Catches lazy-load
# bugs like the mason-nvim-dap-with-no-trigger class that the original
# smoke test concealed by force-loading every plugin.
if timeout 300 env SMOKE_COLD=1 "${NV[@]}" --headless "+luafile $NVIM_DIR/scripts/smoke_test.lua" 2>&1; then
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