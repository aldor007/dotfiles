#!/bin/sh
# shellcheck shell=sh disable=SC1090,SC3043  # dynamic $LOCAL_ENV source; `local` is a macOS /bin/sh pattern
# pam-approve.sh — list and approve GCP PAM (Privileged Access Manager) grant
# requests awaiting YOUR approval, across every project in local/projects.env,
# from an fzf TUI. Complements px: `px glass break` CREATES grant requests;
# this script DECIDES them via `gcloud pam grants approve|deny`.
#
#   Data:     gcloud pam entitlements list + grants search
#             --caller-relationship=can-approve (state APPROVAL_AWAITED).
#             gcloud pam uses the gcloud USER login — run `gcloud auth login`
#             (ADC alone is NOT enough for these calls).
#   Approval: gcloud pam grants approve <grant> --reason R — a reason is always
#             sent (required when the entitlement sets requireApprover-
#             Justification). You CANNOT approve your own requests — GCP
#             rejects them with a permission error. Undecided requests expire
#             after 24h.
#
# Usage:
#   pam-approve.sh                      fzf TUI (default): pick a request, then
#                                       enter/ctrl-a approve · ctrl-d deny ·
#                                       ctrl-r refresh · esc/ctrl-c quit.
#                                       Preview pane shows the full grant;
#                                       without --reason you get a per-grant
#                                       reason prompt (empty = default).
#   pam-approve.sh --list               plain dump of pending requests —
#                                       columns: full grant name, env, project,
#                                       entitlement, requester, duration,
#                                       justification; count line on stderr.
#                                       The smoke-test / machine interface.
#   pam-approve.sh --auto [--interval N] [--yes]
#                                       watch mode: every N seconds (default
#                                       30) approve EVERY pending request in
#                                       scope, logging each decision with a
#                                       UTC timestamp. Prints the scope and
#                                       demands a typed 'yes' unless --yes.
#                                       Recommended: scope it down first:
#                                         pam-approve.sh --auto \
#                                           --entitlement developer-readonly-access
#
# Filters:
#   --project NAME      one project only: env name (dev-master, prod-eu, …) or
#                       exact GCP project id
#   --entitlement STR   only entitlements whose id contains STR (e.g. readonly)
#   --reason TEXT       approval/denial reason (default: "approved by <gcloud
#                       account> via pam-approve.sh"); per-grant prompt when
#                       omitted in TUI mode
#   --users L1,L2,…     (--auto only) requester allowlist for auto-approval:
#                       only requests from these emails are approved; every
#                       other pending request in scope is SKIPPED (logged as
#                       skipped). Comma-separated, no spaces. Omit = everyone.
#                       A leading "user:" prefix is stripped before matching.
#
# Security: auto-approval WEAKENS the breakglass control — it approves every
# request in scope without review; keep the interval sane and prefer
# --entitlement/--users scoping. Every auto decision is logged. No secrets are
# handled: gcloud credentials are used, never echoed. The project list comes
# from gitignored local/projects.env (nothing infra-specific is tracked in git).
#
# Exit codes: 0 ok · 1 runtime error (auth/config/scope) · 2 usage error.

set -eu

LOCAL_ENV="${DOTFILES_LOCAL_FILE:-${DOTFILES_DIR:-$HOME/dotfiles}/local/projects.env}"
[ -f "$LOCAL_ENV" ] || { echo "pam-approve: missing $LOCAL_ENV — run scripts/dotfiles-init.sh" >&2; exit 1; }
. "$LOCAL_ENV"

usage() {
  cat >&2 <<'EOF'
pam-approve.sh — approve GCP PAM grant requests (fzf TUI / --list / --auto)
usage: pam-approve.sh [--list|--auto] [--project NAME] [--entitlement STR]
                      [--reason TEXT] [--interval N] [--yes]
  (no flags)        fzf TUI: enter approve · ctrl-d deny · ctrl-r refresh · esc quit
  --list            dump pending requests (TSV)
  --auto N          approve everything pending in scope every --interval sec
  --users L1,L2     (--auto only) approve only these requesters, skip the rest
  --yes             skip the auto-mode typed confirmation
EOF
}

mode=tui
FILTER_PROJECT=""
FILTER_ENTITLEMENT=""
REASON=""
REASON_GIVEN=0
INTERVAL=30
ASSUME_YES=0
AUTO_USERS=""

while [ $# -gt 0 ]; do
  case "$1" in
    --auto)        mode=auto; shift ;;
    --list)        mode=list; shift ;;
    --project)     [ $# -ge 2 ] || { echo "pam-approve: --project requires a value" >&2; exit 2; }; FILTER_PROJECT=$2; shift 2 ;;
    --entitlement) [ $# -ge 2 ] || { echo "pam-approve: --entitlement requires a value" >&2; exit 2; }; FILTER_ENTITLEMENT=$2; shift 2 ;;
    --reason)      [ $# -ge 2 ] || { echo "pam-approve: --reason requires a value" >&2; exit 2; }; REASON=$2; REASON_GIVEN=1; shift 2 ;;
    --interval)    [ $# -ge 2 ] || { echo "pam-approve: --interval requires a value" >&2; exit 2; }; INTERVAL=$2; shift 2 ;;
    --yes|-y)      ASSUME_YES=1; shift ;;
    --users)       [ $# -ge 2 ] || { echo "pam-approve: --users requires a value" >&2; exit 2; }; AUTO_USERS=$2; shift 2 ;;
    -h|--help)     usage; exit 0 ;;
    *) echo "pam-approve: unknown arg: $1" >&2; usage; exit 2 ;;
  esac
done

case "$INTERVAL" in ''|*[!0-9]*) echo "pam-approve: --interval must be a number of seconds" >&2; exit 2 ;; esac
if [ -n "$AUTO_USERS" ] && [ "$mode" != auto ]; then
  echo "pam-approve: --users only applies to --auto" >&2
  exit 2
fi

# ---- pre-flight ----------------------------------------------------------------
command -v gcloud >/dev/null 2>&1 || { echo "pam-approve: gcloud not found on PATH" >&2; exit 1; }
command -v jq     >/dev/null 2>&1 || { echo "pam-approve: jq not found on PATH (brew install jq)" >&2; exit 1; }
gcloud pam --help >/dev/null 2>&1 || { echo "pam-approve: 'gcloud pam' group unavailable — upgrade the SDK: gcloud components update" >&2; exit 1; }
# gcloud pam acts as the USER login (not ADC) — check it explicitly
gcloud auth print-access-token >/dev/null 2>&1 || { echo "pam-approve: gcloud user login missing — run: gcloud auth login" >&2; exit 1; }
ACCOUNT=$(gcloud config get-value account 2>/dev/null || true)
[ -n "$ACCOUNT" ] || ACCOUNT="unknown"
[ -n "$REASON" ] || REASON="approved by $ACCOUNT via pam-approve.sh"

CACHE=$(mktemp -d "${TMPDIR:-/tmp}/pam-approve.XXXXXX") || { echo "pam-approve: mktemp failed" >&2; exit 1; }
on_exit() { rm -rf "$CACHE"; }
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [ "$mode" = tui ]; then
  command -v fzf >/dev/null 2>&1 || { echo "pam-approve: fzf not found on PATH (./install.sh -i fzf) — or use --list/--auto" >&2; exit 1; }
  _fv=$(fzf --version 2>/dev/null | awk 'NR==1 {print $1}')
  _fmaj=${_fv%%.*}; _frest=${_fv#*.}; _fmin=${_frest%%.*}
  case "${_fmaj}${_fmin}" in ''|*[!0-9]*) _fmaj=9; _fmin=9 ;; esac # unparsable → assume new
  if [ "$_fmaj" -eq 0 ] && [ "$_fmin" -lt 36 ]; then
    echo "pam-approve: fzf $_fv too old — need ≥ 0.36 (become action); upgrade: (cd ~/.fzf && git pull && ./install --all)" >&2
    exit 1
  fi
fi

# ---- scope ---------------------------------------------------------------------
# projects_in_scope: print "env-name project-id" lines from local/projects.env
# (env_*_project vars), honoring --project (env display name or exact id).
projects_in_scope() {
  # plain `set` lists every variable; the anchored sed only matches env_*_project vars
  set | sed -n 's/^env_\([A-Za-z0-9_]*\)_project=\([A-Za-z0-9-]*\)$/\1 \2/p' \
    | while read -r _env _proj; do
        _disp=$(printf '%s' "$_env" | tr '_' '-')
        if [ -n "$FILTER_PROJECT" ] && [ "$_disp" != "$FILTER_PROJECT" ] && [ "$_proj" != "$FILTER_PROJECT" ]; then
          continue
        fi
        printf '%s %s\n' "$_disp" "$_proj"
      done
}

if ! projects_in_scope | grep -q .; then
  _scopemsg=""
  [ -n "$FILTER_PROJECT" ] && _scopemsg=" matching --project '$FILTER_PROJECT'"
  echo "pam-approve: no project in scope${_scopemsg} — check $LOCAL_ENV" >&2
  exit 1
fi

# ---- listing -------------------------------------------------------------------
# list_pending: refresh $CACHE/grants.jsonl (one enriched grant JSON per line:
# the API grant + {env,project,entitlement}) and print TSV rows:
#   full-grant-name \t env \t project \t entitlement \t requester \t duration \t justification(≤80)
list_pending() {
  local _errlog
  _errlog="$CACHE/gcloud-errors.log"
  : > "$_errlog"
  rm -f "$CACHE"/grants-*.jsonl "$CACHE/grants.jsonl"
  # one background pipeline per project, each writing its own jsonl (no shared
  # files → no races). gcloud calls are seconds each; serial across all
  # projects took >200s — unusable for a TUI refresh. Parallel is bounded by
  # the slowest project instead. `wait` stays INSIDE the pipeline's brace
  # group: those background jobs are its children, not the parent shell's.
  projects_in_scope | { while read -r _env _proj; do
    (
      echo "pam-approve: scanning $_env ($_proj)…" >&2
      _ents=$(gcloud pam entitlements list --project="$_proj" --location=global --format=json 2>>"$_errlog") || {
        echo "pam-approve: entitlements list failed for $_proj — details in $_errlog" >&2
        exit 0
      }
      for _ent in $(printf '%s' "$_ents" | jq -r '.[].name | sub(".*/"; "")'); do
        case "$FILTER_ENTITLEMENT" in
          "") : ;;
          *) case "$_ent" in *"$FILTER_ENTITLEMENT"*) : ;; *) continue ;; esac ;;
        esac
        _gj=$(gcloud pam grants search --entitlement="$_ent" --caller-relationship=can-approve \
                --project="$_proj" --location=global --format=json 2>>"$_errlog") || {
          echo "pam-approve: grants search failed for $_proj/$_ent — details in $_errlog" >&2
          continue
        }
        printf '%s' "$_gj" | jq -c --arg env "$_env" --arg proj "$_proj" --arg ent "$_ent" \
          '.[] | select(.state=="APPROVAL_AWAITED") | . + {env:$env, project:$proj, entitlement:$ent}' \
          >> "$CACHE/grants-$_env.jsonl"
      done
    ) &
  done; wait; }
  cat "$CACHE"/grants-*.jsonl > "$CACHE/grants.jsonl" 2>/dev/null || : > "$CACHE/grants.jsonl"
  jq -r '[.name, .env, .project, .entitlement, (.requester // ""), (.requestedDuration // ""),
          ((.justification.unstructuredJustification // "") | gsub("[\n\t]"; " ") | .[0:80])] | @tsv' \
    "$CACHE/grants.jsonl"
}

count_rows() { printf '%s\n' "$1" | grep -c . || true; }
now_utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# user_allowed <requester>: is this requester in the --users allowlist?
# (empty allowlist = everyone; a leading "user:" prefix is stripped)
user_allowed() {
  local _u
  [ -z "$AUTO_USERS" ] && return 0
  _u=${1#user:}
  case ",$AUTO_USERS," in *",$_u,"*) return 0 ;; esac
  return 1
}

# kill_tree <pid>: TERM a pid AND all its descendants (pgrep -P recursion).
# `kill <pid>` alone orphans the mid-scan grandchildren (per-project subshells
# and their gcloud calls); they'd race the EXIT trap's rm -rf of $CACHE.
kill_tree() {
  local _kt_kids _kt_p
  _kt_kids=$(pgrep -P "$1" 2>/dev/null || true)
  for _kt_p in $_kt_kids; do kill_tree "$_kt_p"; done
  kill -TERM "$1" 2>/dev/null || :
}

# ---- TUI -----------------------------------------------------------------------
tui_loop() {
  local _rows _out _action _grant _verb _rsn _line _
  while :; do
    _rows=$(list_pending)
    [ -n "$_rows" ] || echo "pam-approve: no pending requests in scope — ctrl-r re-checks, esc quits" >&2
    if ! _out=$( { [ -n "$_rows" ] && printf '%s\n' "$_rows" || :; } | fzf \
        --delimiter='\t' --with-nth='2..' --layout=reverse --no-multi \
        --header='enter/ctrl-a approve · ctrl-d deny · ctrl-r refresh · esc/ctrl-c quit   (hidden col1: full grant name)' \
        --preview "jq --arg name {1} 'select(.name==\$name) | {name, requester, state, requestedDuration, justification, privilegedAccess, createTime} | with_entries(select(.value!=null))' '$CACHE/grants.jsonl'" \
        --preview-window 'right,60%,wrap' \
        --bind 'enter:become(printf "APPROVE %s" {1})' \
        --bind 'ctrl-a:become(printf "APPROVE %s" {1})' \
        --bind 'ctrl-d:become(printf "DENY %s" {1})' \
        --bind 'ctrl-r:become(echo REFRESH)'); then
      return 0 # esc / ctrl-c / fzf failure → leave the loop
    fi
    _action=${_out%% *}
    _grant=${_out#* }
    case "$_action" in
      REFRESH) continue ;;
      APPROVE|DENY)
        _verb=approve; [ "$_action" = DENY ] && _verb=deny
        _rsn=$REASON
        if [ "$REASON_GIVEN" -eq 0 ]; then
          printf 'pam-approve: %s %s\n' "$_verb" "$_grant"
          printf 'reason (empty = "%s"): ' "$REASON"
          { read -r _line </dev/tty; } 2>/dev/null || _line="" # silent when no usable tty
          [ -n "$_line" ] && _rsn=$_line
        fi
        if [ -z "$_grant" ]; then
          echo "pam-approve: nothing selected" >&2
        elif [ "$_verb" = deny ] && [ -z "$_rsn" ]; then
          echo "pam-approve: deny requires a non-empty reason — skipped" >&2
        else
          echo "pam-approve: ${_verb}ing $_grant …" >&2
          if gcloud pam grants "$_verb" "$_grant" --reason="$_rsn"; then
            echo "pam-approve: $_verb ok" >&2
          else
            echo "pam-approve: $_verb failed — your own requests can't be approved by you; it may also be decided/expired" >&2
          fi
        fi
        # prompt+wait only when a tty is actually usable (silent otherwise)
        printf 'press enter to continue…' >/dev/tty 2>/dev/null && { read -r _ </dev/tty; } 2>/dev/null || :
        ;;
      *) echo "pam-approve: unexpected action '$_action'" >&2 ;;
    esac
  done
}

# ---- auto ----------------------------------------------------------------------
auto_loop() {
  local _scope _confirm _rows _n _g _env _proj _ent _req _dur _just _lppid
  _scope=$(projects_in_scope | awk '{printf "%s ", $1}')
  echo "pam-approve: AUTO-APPROVAL watch mode (account: $ACCOUNT)" >&2
  echo "pam-approve: scope: projects [${_scope% }] entitlement filter [${FILTER_ENTITLEMENT:-<all>}] users allowlist [${AUTO_USERS:-<all>}] interval ${INTERVAL}s reason [$REASON]" >&2
  if [ "$ASSUME_YES" -ne 1 ]; then
    cat >&2 <<'EOF'
pam-approve: WARNING — every pending PAM grant request in the scope above will
pam-approve: be approved automatically, WITHOUT further human review. This
pam-approve: weakens the breakglass control; use --entitlement/--project to
pam-approve: scope it. Type 'yes' to proceed:
EOF
    # tty read (typed confirmation), then piped stdin; both fail silently ⇒ decline
    { read -r _confirm </dev/tty; } 2>/dev/null || { read -r _confirm || _confirm=""; } 2>/dev/null || _confirm=""
    [ "$_confirm" = yes ] || { echo "pam-approve: auto mode declined" >&2; exit 1; }
  fi
  # the scan runs as a background child with a captured pid so INT/TERM can
  # interrupt an in-flight listing promptly — a plain $(list_pending) defers
  # the trap until the whole fan-out returns (observed to swallow Ctrl-C for
  # minutes). kill_tree takes down listing subprocesses too (observed: a bare
  # kill orphans the fan-out, whose writes then race the cache cleanup).
  trap '[ -n "${_lppid:-}" ] && kill_tree "$_lppid"; echo "pam-approve: auto mode stopped ($(now_utc))" >&2; exit 0' INT TERM
  while :; do
    : > "$CACHE/auto-rows.txt"
    list_pending > "$CACHE/auto-rows.txt" &
    _lppid=$!
    wait "$_lppid" 2>/dev/null || :
    _rows=$(cat "$CACHE/auto-rows.txt")
    _n=$(count_rows "$_rows")
    if [ "$_n" -eq 0 ]; then
      echo "pam-approve: $(now_utc) poll: no pending requests" >&2
    else
      printf '%s\n' "$_rows" | while IFS="$(printf '\t')" read -r _g _env _proj _ent _req _dur _just; do
        if ! user_allowed "$_req"; then
          echo "pam-approve: $(now_utc) skipped $_g ($_req → $_ent in $_proj) — not in --users allowlist" >&2
          continue
        fi
        if gcloud pam grants approve "$_g" --reason="$REASON (auto $(now_utc))" >/dev/null 2>>"$CACHE/gcloud-errors.log"; then
          echo "pam-approve: $(now_utc) approved $_g ($_req → $_ent in $_proj)" >&2
        else
          echo "pam-approve: $(now_utc) FAILED to approve $_g ($_req → $_ent in $_proj) — see $CACHE/gcloud-errors.log" >&2
        fi
      done
    fi
    sleep "$INTERVAL"
  done
}

# ---- dispatch ------------------------------------------------------------------
case "$mode" in
  list)
    _rows=$(list_pending)
    if [ -n "$_rows" ]; then
      if command -v column >/dev/null 2>&1; then
        printf '%s\n' "$_rows" | column -t -s "$(printf '\t')"
      else
        printf '%s\n' "$_rows"
      fi
    fi
    echo "pam-approve: $(count_rows "$_rows") pending request(s) in scope ($(now_utc))" >&2
    ;;
  tui)  tui_loop ;;
  auto) auto_loop ;;
esac
