#!/usr/bin/env bash
# shellcheck disable=SC1090  # config/state files are dynamic by design
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
#   pr-summary.sh --analyze  # additionally ask kimchi (LLM) which item to focus
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
DIGEST_FILE=""
# MRs already rendered (pid:iid); used to de-dupe the label section
SHOWN_FILE=$(mktemp)
trap 'rm -f "$SHOWN_FILE" ${DIGEST_FILE:+"$DIGEST_FILE"}' EXIT

SINCE=""; ANALYZE=0
for arg in "$@"; do
  case "$arg" in
    -h|--help) awk 'NR==1{next} /^set -uo pipefail/{exit} {print}' "$0"; exit 0 ;;
    -r|--reset) rm -f "$STATE_FILE"; shift ;;
    -s|--since) SINCE="$2"; shift 2 ;;
    -a|--analyze) ANALYZE=1; shift ;;
    *) ;;
  esac
done

command -v jq >/dev/null || { echo "error: jq required (brew install jq)" >&2; exit 1; }

# When --analyze: stream everything to tty AND tee it into a file for the LLM.
TTY_PRESENT=0; [ -t 1 ] && TTY_PRESENT=1
if [ "$ANALYZE" = "1" ]; then
  DIGEST_FILE=$(mktemp)
  exec > >(tee -a "$DIGEST_FILE")
fi

# ---- colors (tty only) ------------------------------------------------------
if [ "$TTY_PRESENT" = "1" ]; then
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
LAST_GH_OLD="$LAST_GH"; LAST_GL_OLD="$LAST_GL"
# BSD/macOS date uses -v-3d; GNU/Linux uses -d '-3 days'.
date_ago() { # date_ago <n> <unit: d|h>
  if date -u -v-1d +%F >/dev/null 2>&1; then
    case "$2" in
      d) date -u -v-"$1"d +%Y-%m-%dT%H:%M:%SZ ;;
      h) date -u -v-"$1"H +%Y-%m-%dT%H:%M:%SZ ;;
    esac
  else
    case "$2" in
      d) date -u -d "-$1 days" +%Y-%m-%dT%H:%M:%SZ ;;
      h) date -u -d "-$1 hours" +%Y-%m-%dT%H:%M:%SZ ;;
    esac
  fi
}
if [ -n "$SINCE" ]; then
  case "$SINCE" in
    *[dh]) n=${SINCE%[dh]}; BASE_TMP=$(date_ago "$n" "${SINCE: -1}") ;;
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

# A FAILED gh/glab fetch must never look like "no new comments": emit '[]'
# but return non-zero so the caller can flag the run; the baseline is then
# NOT advanced and the failed items are re-fetched next run.
gh_fetch() { # gh_fetch <api args…> — array endpoints only
  # `gh api --paginate` can emit ONE array PER PAGE; jq -s 'add' merges them
  # into a single array (single-page responses pass through unchanged), so
  # count_new always sees ONE array and returns ONE integer. gh's own exit
  # status (PIPESTATUS[0]) governs success; jq trouble degrades to '[]'.
  local out rc
  out=$(gh api --paginate "$@" 2>/dev/null | jq -s 'add // empty' 2>/dev/null); rc=${PIPESTATUS[0]}
  if [ "$rc" -eq 0 ]; then printf '%s\n' "$out"; else printf '[]\n'; fi
  return "$rc"
}
gl_fetch() { # gl_fetch <api args…>
  local out rc
  out=$(glab api "$@" 2>/dev/null); rc=$?
  if [ "$rc" -eq 0 ]; then printf '%s\n' "$out"; else printf '[]\n'; fi
  return "$rc"
}
# GitLab has no client-side pagination; a notes/MR list longer than per_page
# would permanently lose items once the baseline advances. Page until a short
# page (per_page=100 in every caller), merging arrays with jq -s.
gl_fetch_all() { # gl_fetch_all <path> (path may carry a query string)
  local base="$1" sep page out n all='[]'
  case "$base" in *"?") sep="&";; *) sep="?";; esac
  page=1
  while [ "$page" -le 20 ]; do
    if ! out=$(glab api "${base}${sep}page=$page" 2>/dev/null); then
      GL_FETCH_ERR=1
      printf '[]\n'
      return 1
    fi
    n=$(printf '%s' "$out" | jq 'length' 2>/dev/null || echo 0)
    all=$(printf '%s' "$all" "$out" | jq -s 'add // empty' 2>/dev/null || echo '[]')
    [ "$n" -lt 100 ] && break
    page=$((page+1))
  done
  printf '%s\n' "$all"
  return 0
}

GH_FETCH_ERR=0; GL_FETCH_ERR=0

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

TOTAL_NEW=0; TOTAL_PR=0; TOTAL_MR=0

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

    # Merge conflicts + review state in ONE gh pr view call:
    # mergeStateStatus DIRTY = unmergeable as-is; reviewDecision in
    # APPROVED / CHANGES_REQUESTED / REVIEW_REQUIRED (may be empty).
    ms_rd=$(gh pr view "$num" -R "$repo" --json mergeStateStatus,reviewDecision \
            --jq '[.mergeStateStatus, (.reviewDecision // "")] | @tsv' 2>/dev/null || printf '?\t')
    ms=$(printf '%s' "$ms_rd" | cut -f1)
    rd=$(printf '%s' "$ms_rd" | cut -f2)
    if [ "$ms" = "DIRTY" ]; then conflict="${RED}⇄conflict${RST} "; else conflict=""; fi
    case "$rd" in
      APPROVED)           appr="${GRN}✔approved${RST} " ;;
      CHANGES_REQUESTED)  appr="${RED}✋changes${RST} " ;;
      *)                  appr="" ;;
    esac

    # New comments: issue comments + review comments + review bodies, not mine.
    # Each fetch is INDEPENDENT: a failure must not skip later fetches — the
    # old `! c1=… || ! c2=…` short-circuit could leave c2/c3 unset and trip
    # `set -u`. gh_fetch prints '[]' on failure (so counts stay safe) and
    # returns non-zero; the error flag freezes the baseline later.
    c1=$(gh_fetch "repos/$repo/issues/$num/comments?since=$LAST_GH&per_page=100"); rc1=$?
    c2=$(gh_fetch "repos/$repo/pulls/$num/comments?since=$LAST_GH&per_page=100"); rc2=$?
    c3=$(gh_fetch "repos/$repo/pulls/$num/reviews?per_page=100"); rc3=$?
    if [ "$rc1" -ne 0 ] || [ "$rc2" -ne 0 ] || [ "$rc3" -ne 0 ]; then
      GH_FETCH_ERR=1
      echo "  ${YEL}warning: comment fetch failed for ${repo}#${num} — retried next run${RST}" >&2
    fi
    n1=$(count_new "$c1" "$LAST_GH" "$GH_ME" created_at)
    n2=$(count_new "$c2" "$LAST_GH" "$GH_ME" created_at)
    n3=$(printf '%s' "$c3" | jq --arg ts "$LAST_GH" --arg me "$GH_ME" \
          '[ .[] | select(.body != "" and (.submitted_at|sub("\\.[0-9]+Z$";"Z")) > ($ts|sub("\\.[0-9]+Z$";"Z")) and .user.login != $me) ] | length' 2>/dev/null || echo 0)
    n=$((n1+n2+n3)); TOTAL_NEW=$((TOTAL_NEW+n))
    if [ "$n" -gt 0 ]; then cmm="${BLD}${BLU}+$n new${RST}"; else cmm="${DIM}no new${RST}"; fi
    printf ' %s %s%s %-28s %s · %s\n       %s\n' \
      "$ci_line" "$conflict" "$appr" "${repo}#${num}" "$cmm" "$title" "${DIM}${url}${RST}"
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

  # approval state (approvals API: .approved bool, .approved_by arr)
  appr=$(glab api "projects/$pid/merge_requests/$iid/approvals" 2>/dev/null \
         | jq -r 'if .approved then "yes" else "no" end' 2>/dev/null || echo '?')
  if [ "$appr" = "yes" ]; then appr="${GRN}✔approved${RST} "; else appr=""; fi

  notes=$(gl_fetch_all "projects/$pid/merge_requests/$iid/notes?per_page=100&sort=asc&order_by=updated_at")
  if [ $? -ne 0 ]; then GL_FETCH_ERR=1; fi
  n=$(count_new "$notes" "$LAST_GL" "$GL_ME" updated_at)
  TOTAL_NEW=$((TOTAL_NEW+n))
  if [ "$n" -gt 0 ]; then cmm="${BLD}${BLU}+$n new${RST}"; else cmm="${DIM}no new${RST}"; fi
  printf ' %s %s%s %-28s %s · %s\n       %s\n' \
    "$ci_line" "$conflict" "$appr" "${ns}!${iid}" "$cmm" "$title" "${DIM}${url}${RST}"
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
    TAGGED=$(gl_fetch_all "merge_requests?scope=all&state=opened&labels=$enc&per_page=100&order_by=updated_at&sort=desc")
    if [ $? -ne 0 ]; then GL_FETCH_ERR=1; fi
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
# Baselines advance ONLY when the whole run fetched cleanly; a failed fetch
# keeps the previous baseline so nothing is silently skipped.
if [ "$GH_FETCH_ERR" = 1 ] || [ "$GL_FETCH_ERR" = 1 ]; then
  echo "${YEL}WARNING: some comment fetches failed — baselines NOT advanced (rerun to pick them up)${RST}" >&2
fi
mkdir -p "$STATE_DIR"
{
  if [ "$GH_FETCH_OK" = 1 ] && [ "$GH_FETCH_ERR" != 1 ]; then
    echo "LAST_GH=$TODAY"
  else
    echo "LAST_GH=${LAST_GH_OLD:-$LAST_GH}"
  fi
  if [ "$GL_FETCH_OK" = 1 ] && [ "$GL_FETCH_ERR" != 1 ]; then
    echo "LAST_GL=$TODAY"
  else
    echo "LAST_GL=${LAST_GL_OLD:-$LAST_GL}"
  fi
} > "$STATE_FILE"

echo
TAG_EXTRA=""; [ "${TOTAL_TAG:-0}" -gt 0 ] && TAG_EXTRA=", +${TOTAL_TAG} labelled"
echo "${BLD}total: ${TOTAL_PR} PRs (GitHub), ${TOTAL_MR} MRs (GitLab)${TAG_EXTRA}, ${TOTAL_NEW} new comments since baseline${RST}"
[ -z "${GL_TAGS:-}" ] && echo "${DIM}(tip: set GL_TAGS in $CFG_FILE to also list MRs by label)${RST}"
echo "${DIM}baseline for 'new comments': since $LAST_GH → now $TODAY (state: $STATE_FILE)${RST}"
[ "$FIRST_RUN" = 1 ] && echo "${DIM}(first run — baseline initialized; future runs show the delta)${RST}"

# ---- optional LLM focus analysis -------------------------------------------
if [ "$ANALYZE" = "1" ]; then
  echo
  echo "${BLD}=== AI FOCUS (kimchi) ===${RST}"
  if ! command -v kimchi >/dev/null; then
    echo "${YEL}  kimchi not on PATH — skipping analysis${RST}"
    exit 0
  fi
  PROMPT="You are my personal PR triage assistant. Below is a digest of my open PRs (GitHub) and MRs (GitLab). Per item you see: CI state, a conflict flag, an approved flag, and how many new comments arrived since my last check.

Decision policy, most to least important:
1) Feedback from people (new comments) — unblock the discussion or address what reviewers asked.
2) Items that are DONE pending my action to ship: green pipeline + approved + no conflict → propose merging or closing.
3) Broken mechanics: failed pipeline or merge conflict → fix code or rebase.
4) Drafts with nothing pending → keep parked, do not promote.
5) The labelled section is team radar — only mention if something there affects me.

Task: name THE ONE item I should focus on first today and why (max 3 sentences, cite concrete evidence from the digest), then an ordered next-up list of at most 4 items with one-line justification each. If the digest says 'first run' or there is no meaningful comment-delta window, say so instead of fabricating statistics. Be terse. No preamble.

DIGEST:
"
  CLEAN=$(sed $'s/\x1b\\[[0-9;]*m//g' "$DIGEST_FILE")
  printf '%s\n' "$PROMPT$CLEAN" | kimchi --print --no-session --thinking minimal 2>/dev/null \
    || echo "${YEL}  kimchi analysis failed (timeout/auth)${RST}"
fi
exit 0
