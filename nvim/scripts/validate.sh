#!/usr/bin/env bash
# Validates the Neovim config end-to-end:
#   1. Lua syntax of every config/plugin file
#   2. Clean headless startup
#   3. Plugin sync (installs/updates)
#   4. Per-plugin smoke test (scripts/smoke_test.lua)
#   5. checkhealth capture
#   6. Startup time measurement
set -u
cd "$(dirname "$0")/.."

fail=0
step() { echo; echo "==> $1"; }

step "1/6 Lua syntax check"
for f in init.lua lua/config/*.lua lua/plugins/*.lua scripts/smoke_test.lua; do
  if luac -p "$f" 2>/dev/null || nvim -l /dev/stdin <<< "assert(loadfile('$f'))" >/dev/null 2>&1; then
    echo "OK    $f"
  else
    echo "FAIL  $f"; fail=1
  fi
done

step "2/6 Clean headless startup"
if nvim --headless "+qa" >/dev/null 2>&1; then
  echo "OK    nvim --headless +qa"
else
  echo "FAIL  startup errors:"; nvim --headless "+qa" 2>&1 | head -20; fail=1
fi

step "3/6 lazy.nvim sync (install/update plugins)"
if timeout 600 nvim --headless "+Lazy! sync" +qa >/dev/null 2>&1; then
  echo "OK    Lazy sync"
else
  echo "FAIL  Lazy sync"; fail=1
fi

step "4/6 Plugin smoke test"
if nvim --headless "+luafile scripts/smoke_test.lua" 2>&1; then
  :
else
  fail=1
fi

step "5/6 checkhealth (errors only)"
if timeout 120 nvim --headless "+checkhealth" "+w! /tmp/nvim-health.txt" +qa >/dev/null 2>&1; then
  n_err=$(grep -cE "^\s*ERROR" /tmp/nvim-health.txt || true)
  echo "health report: /tmp/nvim-health.txt  (ERROR lines: $n_err)"
else
  echo "FAIL  checkhealth run"; fail=1
fi

step "6/6 Startup time"
nvim --headless --startuptime /tmp/nvim-startup.txt "+qa" >/dev/null 2>&1
grep "NVIM STARTED" /tmp/nvim-startup.txt | tail -1

echo
if [ "$fail" -eq 0 ]; then
  echo "ALL CHECKS PASSED"
else
  echo "VALIDATION FAILED"
  exit 1
fi
