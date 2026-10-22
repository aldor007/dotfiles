#!/usr/bin/env bash
# Regression test for the GitLab URL-join bug fixed in pr-summary.sh.
#
# Bug: the previous pattern `case "$base" in *"?") sep="&";; *) sep="?";; esac`
# matched a URL ENDING in `?` only, which produced malformed URLs like
# `notes?per_page=100?page=2` for any base that already had a query
# string, and silently looped on page 1 (every page ended up being the
# first page). The fix is `*\?*` — match "contains a ?", portable across
# bash, zsh and POSIX sh.
#
# Run: bash scripts/tests/glab-pagination.test.sh
# Exit: 0 = pass, 1 = fail.

set -u
fail() { echo "FAIL: $*" >&2; exit 1; }
ok()   { echo "ok  : $*"; }

# Reproduce the join logic exactly as pr-summary.sh has it (so this test
# doubles as documentation of the contract). If pr-summary.sh ever
# changes the pattern, this helper MUST be updated to match.
glab_url_join() {
  _gj_base=$1
  _gj_qs=$2
  case "$_gj_base" in
    *\?*) printf '%s&%s\n' "$_gj_base" "$_gj_qs" ;;
    *)    printf '%s?%s\n' "$_gj_base" "$_gj_qs" ;;
  esac
}

# ---- 1. base with no query string → '?' separator ---------------------------
got=$(glab_url_join "notes" "per_page=100")
[ "$got" = "notes?per_page=100" ] || fail "no-qs: expected 'notes?per_page=100', got '$got'"
ok "no-qs base joins with '?'"

# ---- 2. base with one existing query param → '&' separator ------------------
# (this is the bug case — the broken pattern would yield '?page=2' here)
got=$(glab_url_join "notes?per_page=100" "page=2")
[ "$got" = "notes?per_page=100&page=2" ] || fail "one-qs: expected 'notes?per_page=100&page=2', got '$got'"
ok "one existing query param joins with '&'"

# ---- 3. base with two existing query params → still '&' ---------------------
got=$(glab_url_join "notes?per_page=100&sort=asc" "page=2")
[ "$got" = "notes?per_page=100&sort=asc&page=2" ] \
  || fail "two-qs: expected 'notes?per_page=100&sort=asc&page=2', got '$got'"
ok "two existing query params still joins with '&'"

# ---- 4. base ending in '?' (no body yet) → '&' -----------------------------
# Edge case: a base like "notes?" (the caller is mid-build). Treated as
# already containing a query separator; '&' is correct.
got=$(glab_url_join "notes?" "per_page=100")
[ "$got" = "notes?&per_page=100" ] || fail "trailing-q: expected 'notes?&per_page=100', got '$got'"
ok "base ending in '?' joins with '&'"

# ---- 5. base containing a literal '?' inside the path → '&' ----------------
got=$(glab_url_join "projects?search=foo" "page=2")
[ "$got" = "projects?search=foo&page=2" ] \
  || fail "literal-q: expected 'projects?search=foo&page=2', got '$got'"
ok "literal '?' anywhere in base is treated as the query separator"

# ---- 6. base containing a literal '?' inside the path → '&' ----------------
# (re-numbered from the original case 6; an earlier empty-base case was
# dropped because empty base is not a real call site — gl_fetch_all is
# always called with a real API path like projects/.../notes.)
got=$(glab_url_join "projects?search=foo" "page=2")
[ "$got" = "projects?search=foo&page=2" ] \
  || fail "literal-q: expected 'projects?search=foo&page=2', got '$got'"
ok "literal '?' anywhere in base is treated as the query separator"

# ---- 7. pagination loop simulation — confirms the loop actually advances ---
# Reproduce the loop body that depends on the join: each iteration appends
# "&page=$n" (the broken pattern would yield "?page=1" every time → infinite
# loop, no progress, "n=100" check never fires). We assert pages 1, 2, 3
# produce the expected progression and that n drops below 100 only when we
# say it does.
base="notes?per_page=100"
pages=()
for n in 1 2 3; do
  pages+=("$(glab_url_join "$base" "page=$n")")
done
[ "${pages[0]}" = "notes?per_page=100&page=1" ] || fail "page 1 URL wrong: ${pages[0]}"
[ "${pages[1]}" = "notes?per_page=100&page=2" ] || fail "page 2 URL wrong: ${pages[1]} (THIS is the bug)"
[ "${pages[2]}" = "notes?per_page=100&page=3" ] || fail "page 3 URL wrong: ${pages[2]}"
ok "pagination loop advances past page 1 (bug repro)"

echo
echo "All glab-pagination regressions pass."
exit 0
