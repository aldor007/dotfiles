#!/usr/bin/env bash
# pr-summary.sh — summary of open PRs (GitHub) + MRs (GitLab):
#   per item: title, repo#number, pipeline/CI state, merge-conflict flag,
#   and NEW comments (comments, review comments, review bodies) since the
#   last run.
#
# Baselines (last-run timestamps) persist in:
#   ${XDG_STATE_HOME:-~/.local/state}/pr-summary/last-run.env
# Optional config (sourced if present):
#   ${XDG_CONFIG_HOME:-~/.config}/pr-summary/config
#   GL_TAGS="label A,label B" — extra section: open GitLab MRs carrying
#   these labels (any author, any project).
#
# Usage:
#   pr-summary.sh            # normal run (updates baselines)
#   pr-summary.sh --since 7d # pretend last run was N days/hours ago (e.g. 7d, 12h)
#   pr-summary.sh --reset    # forget baselines, act like first run
#   pr-summary.sh --help
#
# Requires: jq, gh (authed on github.com), glab (authed, default host set).

set -uo pipefail

# ---- config -----------------------------------------------------------------
STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/pr-summary"
STATE_FILE="$STATE_DIR/last-run.env"
CFG_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/pr-summary/config"
[ -f "$CFG_FILE" ] && . "$CFG_FILE"

# MRs already rendered (pid:iid); used to de-dupe the label section
SHOWN_FILE=$(mktemp)
trap 'rm -f "$SHOWN_FILE"' EXIT

SINCE=""
for arg in "$@"; do
  case "$arg" in
    -h|--help) awk 'NR==1{next} /^set -uo pipefail/{exit} {print}' "$0"; exit 0 ;;
    -r|--reset) rm -f "$STATE_FILE"; shift ;;
    -s|--since) SINCE="$2"; shift 2 ;;
    *) ;;
  esac
done

command -v jq >/dev/null || { echo "error: jq required (brew install jq)" >&2; exit 1; }

# ---- colors (tty only) ------------------------------------------------------
if [ -t 1 ]; then
  GRN=$(tput setaf 2); RED=$(tput setaf 1); YEL=$(tput setaf 3)
  BLU=$(tput setaf 4); BLD=$(tput bold); DIM=$(tput dim); RST=$(tput sgr0)
else
  GRN=""; RED=""; YEL=""; BLU=""; BLD=""; DIM=""; RST=""
fi

TODAY=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LAST_GH=""; LAST_GL=""
[ -f "$STATE_FILE" ] && . "$STATE_FILE"
FIRST_RUN=0
if [ -z "$LAST_GH" ] && [ -z "$LAST_GL" ]; then
  FIRST_RUN=1
  LAST_GH="$TODAY"; LAST_GL="$TODAY"
fi
if [ -n "$SINCE" ]; then
  case "$SINCE" in
    *[dh]) n=${SINCE%[dh]}; case "$SINCE" in
             *d) BASE_TMP=$(date -u -v-"${n}"d +%Y-%m-%dT%H:%M:%SZ) ;;
             *h) BASE_TMP=$(date -u -v-"${n}"H +%Y-%m-%dT%H:%M:%SZ) ;;
           esac ;;
    *)     BASE_TMP="$SINCE" ;;  # raw ISO timestamp allowed
  esac
  LAST_GH="$BASE_TMP"; LAST_GL="$BASE_TMP"; FIRST_RUN=0
fi

# count JSON array items whose timestamp > $2 and author != $3
# $1 = json, $2 = baseline ts, $3 = my username, $4 = ts field
count_new() {
  printf '%s' "$1" | jq --arg ts "$2" --arg me "$3" --arg f "$4" \
    '[ .[] | select((.[ $f ] | sub("\\.[0-9]+Z$";"Z")) > ($ts | sub("\\.[0-9]+Z$";"Z"))
                    and (.user.login // .author.username) != $me
                    and (.system // false | not)) ] | length' 2>/dev/null || echo 0
}

# ---- GitHub -----------------------------------------------------------------
GH_FETCH_OK=1
if command -v gh >/dev/null && gh auth status >/dev/null 2>&1; then
  GH_ME=$(gh api user --jq .login 2>/dev/null) || GH_FETCH_OK=0
  if [ "$GH_FETCH_OK" = 1 ]; then
    GH_PRS=$(gh search prs --author "@me" --state open --limit 100 \
              --json number,title,url,updatedAt,commentsCount,repository 2>/dev/null) || GH_FETCH_OK=0
  fi
else
  GH_FETCH_OK=0
fi

TOTAL_NEW=0; TOTAL_PR=0; TOTAL_MR=0; GH_WARN=""

echo "${BLD}=== GITHUB (github.com · @${GH_ME:-?}) ===${RST}"
if [ "$GH_FETCH_OK" != 1 ]; then
  echo "${YEL}  gh not authenticated/broken — skipping (run: gh auth status)${RST}"
else
  N=$(printf '%s' "$GH_PRS" | jq 'length')
  TOTAL_PR=$N
  [ "$N" -eq 0 ] && echo "${DIM}  no open PRs${RST}"
  while read -r pr; do
    repo=$(printf '%s' "$pr" | jq -r '.repository.nameWithOwner')
    num=$(printf '%s'  "$pr" | jq -r '.number')
    title=$(printf '%s' "$pr" | jq -r '.title')
    url=$(printf '%s'  "$pr" | jq -r '.url')

    # CI state: counts by bucket (pass/fail/pending/cancel/skipping)
    ci=$(gh pr checks "$num" -R "$repo" --json bucket 2>/dev/null \
        | jq -r 'group_by(.bucket) | map("\(length)\(.[0].bucket)") | join(" ")' 2>/dev/null)
    if [ -z "$ci" ]; then ci_line="${DIM}no checks${RST}"
    else
      case "$ci" in
        *fail*)    ci_line="${RED}✗ $ci${RST}" ;;
        *pending*) ci_line="${YEL}… $ci${RST}" ;;
        *)         ci_line="${GRN}✓ $ci${RST}" ;;
      esac
    fi

    # Merge conflicts? (mergeStateStatus DIRTY = unmergeable as-is)
    ms=$(gh pr view "$num" -R "$repo" --json mergeStateStatus --jq '.mergeStateStatus' 2>/dev/null || echo '?')
    if [ "$ms" = "DIRTY" ]; then conflict="${RED}⇄conflict${RST} "; else conflict=""; fi

    # New comments: issue comments + review comments + review bodies, not mine
    c1=$(gh api "repos/$repo/issues/$num/comments?since=$LAST_GH&per_page=100" 2>/dev/null || echo '[]')
    c2=$(gh api "repos/$repo/pulls/$num/comments?since=$LAST_GH&per_page=100" 2>/dev/null || echo '[]')
    c3=$(gh api "repos/$repo/pulls/$num/reviews?per_page=100" 2>/dev/null || echo '[]')
    n1=$(count_new "$c1" "$LAST_GH" "$GH_ME" created_at)
    n2=$(count_new "$c2" "$LAST_GH" "$GH_ME" created_at)
    n3=$(printf '%s' "$c3" | jq --arg ts "$LAST_GH" --arg me "$GH_ME" \
          '[ .[] | select(.body != "" and (.submitted_at|sub("\\.[0-9]+Z$";"Z")) > ($ts|sub("\\.[0-9]+Z$";"Z")) and .user.login != $me) ] | length' 2>/dev/null || echo 0)
    n=$((n1+n2+n3)); TOTAL_NEW=$((TOTAL_NEW+n))
    if [ "$n" -gt 0 ]; then cmm="${BLD}${BLU}+$n new${RST}"; else cmm="${DIM}no new${RST}"; fi
    printf ' %s %s %-28s %s · %s\n       %s\n' \
      "$ci_line" "$conflict" "${repo}#${num}" "$cmm" "$title" "${DIM}${url}${RST}"
  done < <(printf '%s' "$GH_PRS" | jq -c '.[]')
fi
echo

# Render one GitLab MR entry (pipeline, conflict flag, new-comment count)
# and register it in the shown-set for the label section's de-dup.
# Uses/updates globals: LAST_GL GL_ME SHOWN_FILE TOTAL_NEW.
render_mr() {
  local mr="$1" pid iid title url ns pl notes n ci_line conflict cmm
  pid=$(printf '%s' "$mr" | jq -r '.project_id')
  iid=$(printf '%s' "$mr" | jq -r '.iid')
  title=$(printf '%s' "$mr" | jq -r '.title')
  url=$(printf '%s'  "$mr" | jq -r '.web_url')
  ns=$(printf '%s'  "$mr" | jq -r '.references.full' | sed 's/![0-9]*$//')
  echo "$pid:$iid" >> "$SHOWN_FILE"

  pl=$(glab api "projects/$pid/merge_requests/$iid" 2>/dev/null | jq -r '.head_pipeline.status // "none"' || echo '?')
  case "$pl" in
    success)              ci_line="${GRN}✓ success${RST}" ;;
    failed|canceled)      ci_line="${RED}✗ $pl${RST}" ;;
    running|pending|preparing|waiting_for_resource|created|manual) ci_line="${YEL}… $pl${RST}" ;;
    none|'')              ci_line="${DIM}no pipeline${RST}" ;;
    *)                    ci_line="${DIM}$pl${RST}" ;;
  esac

  # merge conflicts? (has_conflicts ships in every MR list/detail payload)
  if [ "$(printf '%s' "$mr" | jq -r '.has_conflicts // false')" = "true" ]; then
    conflict="${RED}⇄conflict${RST} "
  else
    conflict=""
  fi

  notes=$(glab api "projects/$pid/merge_requests/$iid/notes?per_page=100&sort=asc&order_by=updated_at" 2>/dev/null || echo '[]')
  n=$(count_new "$notes" "$LAST_GL" "$GL_ME" updated_at)
  TOTAL_NEW=$((TOTAL_NEW+n))
  if [ "$n" -gt 0 ]; then cmm="${BLD}${BLU}+$n new${RST}"; else cmm="${DIM}no new${RST}"; fi
  printf ' %s %s %-28s %s · %s\n       %s\n' \
    "$ci_line" "$conflict" "${ns}!${iid}" "$cmm" "$title" "${DIM}${url}${RST}"
}

# ---- GitLab -----------------------------------------------------------------
GL_FETCH_OK=1; GL_ME=""; GL_PRS="[]"
if command -v glab >/dev/null && glab auth status >/dev/null 2>&1; then
  GL_HOST=$(glab config get host 2>/dev/null)
  # glab api picks its host from the repo remote otherwise — cwd is arbitrary
  # here, so pin it to the configured default host explicitly.
  export GITLAB_HOST="$GL_HOST"
  # NB: glab api has no --jq (that's gh) — it prints JSON; filter with jq.
  GL_ME=$(glab api user 2>/dev/null | jq -r .username)
  GL_UID=$(glab api user 2>/dev/null | jq -r .id)
  if [ -z "$GL_UID" ]; then GL_FETCH_OK=0; else
    GL_PRS=$(glab api "merge_requests?scope=all&state=opened&author_id=$GL_UID&per_page=100&order_by=updated_at&sort=desc" 2>/dev/null) \
      || GL_FETCH_OK=0
  fi
else
  GL_FETCH_OK=0
fi

echo "${BLD}=== GITLAB (${GL_HOST:-?} · @${GL_ME:-?}) ===${RST}"
if [ "$GL_FETCH_OK" != 1 ]; then
  echo "${YEL}  glab not authenticated/broken — skipping (run: glab auth status)${RST}"
else
  N=$(printf '%s' "$GL_PRS" | jq 'length' 2>/dev/null || echo 0)
  TOTAL_MR=$N
  [ "$N" -eq 0 ] && echo "${DIM}  no open MRs${RST}"
  while read -r mr; do
    render_mr "$mr"
  done < <(printf '%s' "$GL_PRS" | jq -c '.[]')
fi
echo

# ---- GitLab: MRs by configured label(s) --------------------------------------
# GL_TAGS from $CFG_FILE (comma-separated; spaces inside label names are OK).
if [ "$GL_FETCH_OK" = 1 ] && [ -n "${GL_TAGS:-}" ]; then
  echo "${BLD}=== GITLAB labelled (${GL_TAGS}) ===${RST}"
  TOTAL_TAG=0
  IFS=',' read -ra TAG_ARR <<< "$GL_TAGS"
  for tag in "${TAG_ARR[@]}"; do
    tag=$(printf '%s' "$tag" | sed 's/^ *//;s/ *$//')
    [ -z "$tag" ] && continue
    enc=$(printf '%s' "$tag" | jq -sRr @uri)
    TAGGED=$(glab api "merge_requests?scope=all&state=opened&labels=$enc&per_page=100&order_by=updated_at&sort=desc" 2>/dev/null || echo '[]')
    cnt=$(printf '%s' "$TAGGED" | jq 'length' 2>/dev/null || echo 0)
    if [ "$cnt" -eq 0 ]; then
      echo "${DIM}  [$tag] — no open MRs${RST}"
      continue
    fi
    while read -r mr; do
      pid=$(printf '%s' "$mr" | jq -r '.project_id')
      iid=$(printf '%s' "$mr" | jq -r '.iid')
      grep -qx "$pid:$iid" "$SHOWN_FILE" && continue  # already listed above
      TOTAL_TAG=$((TOTAL_TAG+1))
      render_mr "$mr"
    done < <(printf '%s' "$TAGGED" | jq -c '.[]')
  done
  [ "${TOTAL_TAG:-0}" -eq 0 ] && echo "${DIM}  (nothing beyond your authored MRs above)${RST}"
fi

# ---- persist baselines ------------------------------------------------------
mkdir -p "$STATE_DIR"
{
  [ "$GH_FETCH_OK" = 1 ] && echo "LAST_GH=$TODAY" || echo "LAST_GH=${LAST_GH}"
  [ "$GL_FETCH_OK" = 1 ] && echo "LAST_GL=$TODAY" || echo "LAST_GL=${LAST_GL}"
} > "$STATE_FILE"

echo
TAG_EXTRA=""; [ "${TOTAL_TAG:-0}" -gt 0 ] && TAG_EXTRA=", +${TOTAL_TAG} labelled"
echo "${BLD}total: ${TOTAL_PR} PRs (GitHub), ${TOTAL_MR} MRs (GitLab)${TAG_EXTRA}, ${TOTAL_NEW} new comments since baseline${RST}"
[ -z "${GL_TAGS:-}" ] && echo "${DIM}(tip: set GL_TAGS in $CFG_FILE to also list MRs by label)${RST}"
echo "${DIM}baseline for 'new comments': since $LAST_GH → now $TODAY (state: $STATE_FILE)${RST}"
[ "$FIRST_RUN" = 1 ] && echo "${DIM}(first run — baseline initialized; future runs show the delta)${RST}"
exit 0
