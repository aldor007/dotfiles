#!/usr/bin/env bash
# scripts/tests/herdr-action-validate.test.sh
# Shell-side coverage for the kimchi-remote action validator. The
# smoke_test.lua runs inside nvim and can't exercise shell pipelines;
# this test asserts each of the four documented scenarios (installed
# ok, missing plugin, installed with renamed action, malformed JSON)
# using synthetic manifests piped via stdin.
#
# Run: bash scripts/tests/herdr-action-validate.test.sh

set -u
FAIL=0
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VALIDATOR="$SCRIPT_DIR/../herdr-action-validate.py"

# Sanity: validator must parse (Python syntax check).
if ! python3 -c "import ast; ast.parse(open('$VALIDATOR').read())"; then
  echo "FAIL: $VALIDATOR does not parse" >&2
  exit 2
fi

# Helper: pipe $1 (JSON manifest) and $2 (args) through the validator;
# capture stdout, stderr, and exit code.
run_validator() {
  local json="$1"; shift
  local args=("$@")
  local out err rc
  out=$(echo "$json" | python3 "$VALIDATOR" "${args[@]}" 2>/tmp/validate.err)
  rc=$?
  err=$(cat /tmp/validate.err)
  rm -f /tmp/validate.err
  echo "$out"
  echo "$err" >&2
  return "$rc"
}

# Case 1: synthetic manifest with all 4 expected actions → exit 0.
JSON1='{"result":{"actions":[
  {"action_id":"open-picker","plugin_id":"kimchi-remote"},
  {"action_id":"refresh-mine","plugin_id":"kimchi-remote"},
  {"action_id":"open-worker-shell","plugin_id":"kimchi-remote"},
  {"action_id":"open-new-workspace","plugin_id":"kimchi-remote"}
]}}'
out=$(run_validator "$JSON1" --plugin=kimchi-remote \
  --expected=open-picker --expected=refresh-mine \
  --expected=open-worker-shell --expected=open-new-workspace)
rc=$?
assert_output() {
  local label="$1"; local want_rc="$2"; local need="$3"; local got_rc="$4"; local out="$5"
  if [ "$got_rc" -ne "$want_rc" ]; then
    echo "FAIL: $label expected exit $want_rc got $got_rc"
    echo "  output: $out"
    FAIL=$((FAIL + 1))
  fi
  if [ -n "$need" ] && ! echo "$out" | grep -q -- "$need"; then
    echo "FAIL: $label output missing '$need'"
    echo "  output: $out"
    FAIL=$((FAIL + 1))
  fi
  if [ "$got_rc" -eq "$want_rc" ] && [ -z "$need" ]; then
    echo "OK   $label"
  fi
  if [ "$got_rc" -eq "$want_rc" ] && [ -n "$need" ]; then
    echo "OK   $label (output contained required text)"
  fi
}

assert_output "case1-all-present" 0 "all expected present" "$rc" "$out"

# Case 2: synthetic manifest missing one action → exit 1 + diagnostic.
JSON2='{"result":{"actions":[
  {"action_id":"open-picker","plugin_id":"kimchi-remote"}
]}}'
out=$(run_validator "$JSON2" --plugin=kimchi-remote \
  --expected=open-picker --expected=refresh-mine 2>&1)
rc=$?
assert_output "case2-one-missing" 1 "refresh-mine" "$rc" "$out"

# Case 3: plugin absent (empty actions) → exit 1 + diagnostic.
JSON3='{"result":{"actions":[]}}'
out=$(run_validator "$JSON3" --plugin=kimchi-remote \
  --expected=open-picker 2>&1)
rc=$?
assert_output "case3-plugin-absent" 1 "open-picker" "$rc" "$out"

# Case 4: renamed action (refresh-mine → refresh_owned) → exit 1 + diagnostic.
JSON4='{"result":{"actions":[
  {"action_id":"open-picker","plugin_id":"kimchi-remote"},
  {"action_id":"refresh_owned","plugin_id":"kimchi-remote"}
]}}'
out=$(run_validator "$JSON4" --plugin=kimchi-remote \
  --expected=open-picker --expected=refresh-mine 2>&1)
rc=$?
assert_output "case4-renamed" 1 "refresh-mine" "$rc" "$out"

# Case 5: malformed JSON → exit 2 (parse error).
out=$(echo "{not-json" | python3 "$VALIDATOR" --plugin=p --expected=x 2>&1)
rc=$?
assert_output "case5-malformed" 2 "could not parse" "$rc" "$out"

if [ "$FAIL" -gt 0 ]; then
  echo
  echo "$FAIL validator test case(s) failed"
  exit 1
fi
echo
echo "All validator test cases passed"
exit 0
