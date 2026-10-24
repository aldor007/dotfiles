#!/bin/sh
# shellcheck shell=sh disable=SC1090,SC3043  # dynamic $LOCAL_ENV source; `local` is a macOS /bin/sh pattern
# sql-db.sh — connect to a CAST AI Cloud SQL Postgres instance from nvim/psql,
# any environment, without cloud-sql-proxy and without stored passwords.
#
#   Tunnel:  CAST AI CLI `px glass proxy` — IMPORTANT: this is an interactive
#            TUI wizard (bubbletea) and REQUIRES a real terminal. Start it with
#            `--fg` in a terminal and leave it running; Ctrl-C stops it.
#            Needs GCP ADC (gcloud auth application-default login); PAM grants
#            are assumed active (--skip-grants if already elevated, or run
#            `px glass break` first).
#   Auth:    Cloud SQL IAM — the short-lived gcloud USER token is used as the
#            password, minted fresh per run. Read-only by default; elevate
#            to write with `px glass database` (breakglass_readwrite, windowed).
#            Tunnel auth (ADC) and DB auth (user login) are checked separately,
#            each with its own clear error.
#
# Security: no complete DSN is ever echoed to a terminal. The raw DSN is only
# produced on stdout by the explicit machine mode `--dsn` (used by nvim's
# config/db.lua). psql receives the password via $PGPASSWORD; rainfrog and
# vi-sql take the DSN as an argument (their only supported interface — token
# is short-lived; see README note). Tunnel teardown is PID-file based: --fg
# records its pid, --stop only terminates processes it can verify as px glass.
#
# Usage:
#   sql-db.sh --env dev-master --fg   start the tunnel in THIS terminal
#                                     (wizard; Ctrl-C stops it; run once per env)
#   sql-db.sh [--env X] [--name N]    tunnel must be up; prints redacted
#                                     connection info (never the token)
#   sql-db.sh --env X --dsn           same, but print ONLY the raw DSN
#                                     (machine interface — nvim config/db.lua)
#   sql-db.sh --env X --psql          same, then exec psql (password via env)
#   sql-db.sh --env X --rainfrog      same, then open rainfrog TUI
#   sql-db.sh --env X --visql         same, then open vi-sql TUI
#   Auto-tunnel:                      --dsn/--psql/--rainfrog/--visql start the
#                                     tunnel automatically when it is down — in
#                                     a detached tmux session (sql-db-<env>), px
#                                     wizard auto-driven. psql/rainfrog/vi-sql
#                                     stop that tunnel again on exit; --dsn
#                                     leaves it up (nvim keeps using it).
#                                     --stop removes tmux tunnels too; --fg
#                                     still runs the wizard interactively.
#   Auto-grant:                       when GCP denies the instance lookup (PAM
#                                     grants expire after 1h), the script
#                                     requests developer-readonly-access itself
#                                     via px glass break (tmux-driven wizard)
#                                     and retries. Write access still requires
#                                     the explicit `px glass database` step.
#   vi-sql:                           connects under a script-owned name
#                                     (<env>-<db>) so its saved-connection
#                                     registry never conflicts with manual
#                                     entries or rotated IAM tokens.
#   sql-db.sh --stop                  stop ONLY the px tunnels this script
#                                     started (verified by pidfile); never
#                                     touches unrelated px processes
#   extra flags pass through to px glass proxy (--instance VM, --zone Z,
#                                     --skip-grants, ...)
#
# Defaults: --env dev-master (dev, NOT prod — change explicitly)
# Overrides via env: SQL_DB_PROJECT, SQL_DB_USER, SQL_DB_PORT, SQL_DB_STATE_DIR.

set -eu

# Real names/IDs live in local/projects.env (gitignored; rendered/edited via
# scripts/dotfiles-init.sh) — nothing infra-specific is tracked in git.
LOCAL_ENV="${DOTFILES_LOCAL_FILE:-${DOTFILES_DIR:-$HOME/dotfiles}/local/projects.env}"
[ -f "$LOCAL_ENV" ] || { echo "sql-db: missing $LOCAL_ENV — run scripts/dotfiles-init.sh" >&2; exit 1; }
. "$LOCAL_ENV"

STATE_DIR="${SQL_DB_STATE_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/sql-db}"
PID_DIR="$STATE_DIR/tunnels"

env_lookup() { # env_lookup <env> <project|port>
  local k
  k="env_$(printf '%s' "$1" | tr '-' '_')_$2"
  eval "printf '%s' \"\${$k:-}\""
}
project_for_env() { env_lookup "$1" project; }
port_for_env() { env_lookup "$1" port; }

ENV="dev-master"
env_given=0
NAME=""
DB=""
USER=""
PORT="${SQL_DB_PORT:-}"
PROJECT="${SQL_DB_PROJECT:-}"
mode=print
px_extra=""

while [ $# -gt 0 ]; do
  case "$1" in
    --env)      ENV="$2"; env_given=1; shift 2 ;;
    --name)     NAME="$2"; shift 2 ;;
    --db)       DB="$2"; shift 2 ;;
    --user)     USER="$2"; shift 2 ;;
    --port)     PORT="$2"; shift 2 ;;
    --project)  PROJECT="$2"; shift 2 ;;
    --fg|--foreground) mode=foreground; shift ;;
    --dsn)      mode=dsn; shift ;;
    --psql)     mode=psql; shift ;;
    --rainfrog) mode=rainfrog; shift ;;
    --visql)    mode=visql; shift ;;
    --stop)     mode=stop; shift ;;
    --*)        px_extra="$px_extra $1"; shift ;;
    *) echo "sql-db: unknown arg: $1" >&2; exit 2 ;;
  esac
done

# ---- resolve config (fallbacks only kick in when empty — defaults are empty) --
[ -n "$PROJECT" ] || PROJECT=$(project_for_env "$ENV")
[ -n "$PORT" ] || PORT=$(port_for_env "$ENV")
[ -n "$NAME" ] || NAME="${SQL_SUFFIX:-}"
[ -n "$DB" ] || DB="${DB_NAME:-$NAME}"
[ -n "$USER" ] || USER="${DB_USER:-}"
if [ -z "$USER" ]; then
  echo "sql-db: no DB user — set DB_USER in $LOCAL_ENV" >&2; exit 1
fi
if [ -z "$NAME" ]; then
  echo "sql-db: no instance name — set SQL_SUFFIX in $LOCAL_ENV (run scripts/dotfiles-init.sh)" >&2
  exit 1
fi
if [ -z "$PORT" ]; then
  echo "sql-db: no port for env '$ENV' — set env_${ENV}_port in $LOCAL_ENV (run scripts/dotfiles-init.sh)" >&2
  exit 1
fi

pidfile_for() { printf '%s/px-%s-%s.pid' "$PID_DIR" "$1" "$2"; }
tmux_session_for() { printf 'sql-db-%s' "$(printf '%s' "$1" | tr -c 'a-zA-Z0-9' '-')"; }

# px glass proxy delegates the actual forwarding to `gcloud compute ssh
# -L <port>:… -N`; killing px (or its tmux pane) orphans that ssh chain, which
# then keeps the port bound. Reap it by project+local-port — specific enough
# that only this env's tunnel chain matches.
tunnel_children_reap() { # tunnel_children_reap <project> <port>
  pkill -f ".*--project=$1.*-L $2:" 2>/dev/null || true
}

# Request a time-boxed PAM entitlement grant via `px glass break`, driving its
# bubbletea wizard in a detached tmux pane (px needs a real TTY). Returns 0
# when the wizard reports the grant as active. Only confirmation keystrokes
# are sent — never invented input. Read-only grants only; write elevation
# stays a deliberate `px glass database` step.
pam_grant_request() { # pam_grant_request <project> <entitlement> <justification> <timeout_s>
  _pg_proj=$1; _pg_ent=$2; _pg_just=$3
  command -v tmux >/dev/null 2>&1 || {
    echo "sql-db: tmux missing — request manually: px glass break --project ${_pg_proj} --entitlement ${_pg_ent}" >&2
    return 1
  }
  command -v px >/dev/null 2>&1 || { echo "sql-db: px (CAST CLI) not found on PATH" >&2; return 1; }
  _pg_sess="sql-db-grant"
  tmux has-session -t "$_pg_sess" 2>/dev/null && tmux kill-session -t "$_pg_sess"
  if ! tmux new-session -d -s "$_pg_sess" \
      "exec px glass break --project '$_pg_proj' --entitlement '$_pg_ent' --justification '$_pg_just' --duration 3600"; then
    echo "sql-db: failed to start grant wizard (tmux session $_pg_sess)" >&2
    return 1
  fi
  _pg_deadline=$(( $(date +%s) + $4 ))
  while [ "$(date +%s)" -lt "$_pg_deadline" ]; do
    _pg_pane=$(tmux capture-pane -p -t "$_pg_sess" 2>/dev/null) || break # wizard died
    case "$_pg_pane" in
      *"Access Granted"*)
        tmux kill-session -t "$_pg_sess" 2>/dev/null || true
        echo "sql-db: PAM grant active ($_pg_ent on $_pg_proj, 1h)" >&2
        return 0 ;;
      *"Request grant?"*|*"Press any key"*) tmux send-keys -t "$_pg_sess" Enter ;;
    esac
    sleep 1
  done
  echo "sql-db: grant wizard did not complete in ${4}s — inspect: tmux attach -t ${_pg_sess} (approval may be pending)" >&2
  return 1
}

# Drive the px glass proxy bubbletea wizard in a detached tmux pane until the
# local port opens: accept grant prompts (Enter) and select $3 in the Cloud SQL
# instance list (nudge the highlight, then Enter). Non-zero on timeout or if
# the wizard dies. The wizard pre-fills everything from the flags it was given;
# only confirmation keystrokes are ever sent — never invented input.
wizard_drive() { # wizard_drive <session> <port> <instance> <timeout_s>
  _wd_sess=$1; _wd_port=$2; _wd_inst=$3
  _wd_deadline=$(( $(date +%s) + $4 ))
  while [ "$(date +%s)" -lt "$_wd_deadline" ]; do
    if nc -z 127.0.0.1 "$_wd_port" >/dev/null 2>&1; then return 0; fi
    _wd_pane=$(tmux capture-pane -p -t "$_wd_sess" 2>/dev/null) || return 1
    case "$_wd_pane" in *EXIT:*) return 1 ;; esac
    if printf '%s\n' "$_wd_pane" | grep -q 'Request grant?'; then
      tmux send-keys -t "$_wd_sess" Enter
    elif printf '%s\n' "$_wd_pane" | grep -q 'Select a Cloud SQL instance'; then
      # diff>0 → Down ×diff, diff<0 → Up ×|diff|; 0 (already highlighted) → Enter
      _wd_diff=$(printf '%s\n' "$_wd_pane" | awk -v want="$_wd_inst" '
        {
          if (match($0, /sql-[A-Za-z0-9_-]+/)) {
            name = substr($0, RSTART, RLENGTH)
            n++
            if ($0 ~ />/) cur = n
            if (name == want) wantn = n
          }
        }
        END { if (n && wantn && cur) print wantn - cur }')
      [ -n "$_wd_diff" ] || return 1 # target not in the wizard list
      _wd_i=0
      while [ "$_wd_i" -lt "$_wd_diff" ]; do tmux send-keys -t "$_wd_sess" Down; _wd_i=$((_wd_i+1)); done
      while [ "$_wd_i" -gt "$_wd_diff" ]; do tmux send-keys -t "$_wd_sess" Up; _wd_i=$((_wd_i-1)); done
      tmux send-keys -t "$_wd_sess" Enter
    fi
    sleep 1
  done
  return 1
}

# ---- stop: PID-file-based teardown -------------------------------------------
if [ "$mode" = stop ]; then
  mkdir -p "$PID_DIR"
  stopped=0; stale=0; skipped=0
  # per-env stop (--stop --env X) touches only that tunnel; plain --stop
  # touches every pidfile this script ever recorded
  if [ "$env_given" = 1 ]; then
    candidates="$(pidfile_for "$ENV" "$PORT")"
    # also scan ports for the untracked-warning below
    scan_ports="$ENV"
  else
    # shellcheck disable=SC2125 # glob is intentional: expands in the loop below
    candidates="$PID_DIR"/px-*.pid
    scan_ports="dev-master prod-master prod-eu prod-india"
  fi
  for f in $candidates; do
    [ -e "$f" ] || continue
    tag=$(basename "$f" .pid); pid=$(cat "$f" 2>/dev/null || true)
    # validate the process BEFORE touching metadata: a foreign pid keeps its
    # pidfile so a later --stop can still reason about it
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      cmd=$(ps -p "$pid" -o command= 2>/dev/null || true)
      case "$cmd" in
        *"px glass"*)
          kill "$pid" 2>/dev/null || true
          rm -f "$f"
          echo "sql-db: stopped $tag (pid $pid)"
          stopped=$((stopped + 1)) ;;
        *)
          echo "sql-db: pid $pid ($tag) is NOT a px glass process — left running, pidfile kept"
          skipped=$((skipped + 1)) ;;
      esac
    else
      echo "sql-db: stale pidfile $tag (pid ${pid:-?} not running) — removed"
      rm -f "$f"
      stale=$((stale + 1))
    fi
  done
  # auto-started tmux tunnels (wizard auto-driven; not pidfile tracked) —
  # kill the whole session, then reap the orphaned gcloud ssh forwards
  for e in $scan_ports; do
    tsess=$(tmux_session_for "$e")
    if tmux has-session -t "$tsess" 2>/dev/null; then
      tmux kill-session -t "$tsess"
      echo "sql-db: stopped tmux tunnel $tsess (env $e)"
      stopped=$((stopped + 1))
    fi
    tp=$(port_for_env "$e"); tproj=$(project_for_env "$e")
    [ -n "$tp" ] && [ -n "$tproj" ] || continue
    if pkill -f ".*--project=$tproj.*-L $tp:" 2>/dev/null; then
      echo "sql-db: reaped orphaned ssh tunnel (env $e, port $tp)"
      sleep 1
    fi
  done
  # ports that are open but were never recorded by this script (e.g. started
  # before pidfiles existed, or by another session) — never touch them
  for e in $scan_ports; do
    p=$(port_for_env "$e")
    [ -n "$p" ] || continue
    [ -e "$(pidfile_for "$e" "$p")" ] && continue
    tsess=$(tmux_session_for "$e")
    tmux has-session -t "$tsess" 2>/dev/null && continue
    if nc -z 127.0.0.1 "$p" >/dev/null 2>&1; then
      echo "sql-db: port $p (env $e) is open but untracked — not stopping (stop it manually: Ctrl-C in its terminal)"
    fi
  done
  [ "$stopped" -eq 0 ] && [ "$stale" -eq 0 ] && [ "$skipped" -eq 0 ] \
    && echo "sql-db: no tracked px tunnels to stop"
  exit 0
fi

# ---- identity checks (separate, explicit) ------------------------------------
# Tunnel auth: ADC (px glass uses Application Default Credentials).
if ! gcloud auth application-default print-access-token >/dev/null 2>&1; then
  echo "sql-db: GCP ADC missing (tunnel auth) — run: gcloud auth application-default login" >&2
  exit 1
fi
# DB auth: user login token (Cloud SQL IAM logs in AS the DB_USER principal).
if ! gcloud auth print-access-token >/dev/null 2>&1; then
  echo "sql-db: gcloud user login missing (DB password source) — run: gcloud auth login" >&2
  exit 1
fi

# resolve the Cloud SQL instance live (suffix match, e.g. name~service-a$).
# On an IAM denial (expired PAM grant — they are 1h by design) request a fresh
# read-only grant automatically, then retry; grants propagate within seconds.
_sql_err=$(mktemp)
instance=""
_ri=1
while [ "$_ri" -le 3 ]; do
  instance=$(gcloud sql instances list --project="$PROJECT" --format="value(name)" 2>"$_sql_err" \
    | grep -E "${NAME}$" | head -1 || true)
  [ -n "$instance" ] && break
  if [ "$_ri" = 1 ] && grep -qi 'not authorized\|does not have permission\|permission denied' "$_sql_err"; then
    echo "sql-db: GCP denied Cloud SQL access to $(gcloud config get-value account 2>/dev/null) — requesting read-only PAM grant (1h)..." >&2
    pam_grant_request "$PROJECT" developer-readonly-access \
      "sql-db.sh: read-only Cloud SQL access (env ${ENV}, *${NAME})" 90 || true
    sleep 2 # allow the grant to propagate
  else
    break # not a permission problem (or still denied after renewal)
  fi
  _ri=$((_ri + 1))
done
if [ -z "$instance" ]; then
  echo "sql-db: no instance matching '*${NAME}' in ${PROJECT} (env ${ENV})" >&2
  grep -m1 'ERROR' "$_sql_err" >&2 || true
  rm -f "$_sql_err"
  exit 1
fi
rm -f "$_sql_err"

if [ "$mode" = foreground ]; then
  # one tunnel per env/port — refuse a duplicate wizard
  pidfile=$(pidfile_for "$ENV" "$PORT")
  if [ -e "$pidfile" ]; then
    old=$(cat "$pidfile" 2>/dev/null || true)
    if [ -n "$old" ] && kill -0 "$old" 2>/dev/null; then
      echo "sql-db: tunnel for ${ENV} already running (pid $old) — Ctrl-C it there, or --stop" >&2
      exit 1
    fi
    rm -f "$pidfile" # stale from a crashed run; proceed
  fi
  if nc -z 127.0.0.1 "$PORT" >/dev/null 2>&1; then
    echo "sql-db: port ${PORT} (${ENV}) is already tunneled (auto tmux tunnel or untracked px) — sql-db.sh --stop, then retry" >&2
    exit 1
  fi
  mkdir -p "$PID_DIR"
  # atomic claim — two concurrent --fg for the same env can't both win
  if ! ( set -C; echo "$$" > "$pidfile" ) 2>/dev/null; then
    echo "sql-db: tunnel for ${ENV} already starting/running (pidfile $pidfile) — try again or --stop" >&2
    exit 1
  fi
  command -v px >/dev/null 2>&1 || {
    echo "sql-db: px (CAST CLI) not found on PATH" >&2
    rm -f "$pidfile"
    exit 127
  }
  # token never enters argv or the tunnel; px uses ADC on its own
  echo "sql-db: tunnel ${ENV} (${instance}) on 127.0.0.1:${PORT} — Ctrl-C stops it"
  echo "sql-db: nvim: press ,dD / ,dk (DSN via: sql-db.sh --env ${ENV} --dsn)"
  cleanup() { rm -f "$pidfile"; }
  trap cleanup INT TERM EXIT
  # Run px as our CHILD (not exec): we record its real pid ($!, which IS the
  # px glass process --stop validates against), Ctrl-C/TERM hit our trap so
  # the pidfile is cleaned immediately, and `wait` propagates the exit code.
  # A --stop racing the rewrite below sees "$$" = the shell (not px glass)
  # and correctly refuses to kill it.
  # shellcheck disable=SC2086
  px glass proxy --backend psql --project "$PROJECT" --local-port "$PORT" $px_extra &
  echo "$!" > "$pidfile"
  wait "$!"
  rc=$?
  cleanup
  exit $rc
fi

# ---- connect modes: tunnel must be up (auto-start via tmux when down) --------
auto_started=0
tsess=$(tmux_session_for "$ENV")
if ! nc -z 127.0.0.1 "$PORT" >/dev/null 2>&1; then
  if [ "$mode" = print ]; then
    echo "sql-db: tunnel not up on 127.0.0.1:${PORT} — start it first (this terminal):" >&2
    echo "sql-db:   sql-db.sh --env ${ENV} --fg" >&2
    exit 1
  fi
  command -v tmux >/dev/null 2>&1 || {
    echo "sql-db: tunnel not up and tmux missing — start it manually: sql-db.sh --env ${ENV} --fg" >&2
    exit 1
  }
  command -v px >/dev/null 2>&1 || { echo "sql-db: px (CAST CLI) not found on PATH" >&2; exit 127; }
  # a leftover session whose wizard died blocks the name — restart clean
  tmux has-session -t "$tsess" 2>/dev/null && tmux kill-session -t "$tsess"
  # token never enters argv or the tunnel; px uses ADC on its own and requests
  # its own PAM grants (cloudsql + tunnel) inside the wizard
  # shellcheck disable=SC2086
  if ! tmux new-session -d -s "$tsess" "exec px glass proxy --backend psql --project '$PROJECT' --local-port '$PORT' $px_extra"; then
    echo "sql-db: failed to start tmux tunnel session $tsess" >&2
    exit 1
  fi
  auto_started=1
  echo "sql-db: starting tunnel ${ENV} (${instance}) in tmux session ${tsess} — wizard auto-driven" >&2
  if ! wizard_drive "$tsess" "$PORT" "$instance" 90; then
    echo "sql-db: tunnel wizard did not come up in 90s — inspect: tmux attach -t ${tsess} (or: sql-db.sh --stop)" >&2
    exit 1
  fi
  echo "sql-db: tunnel up on 127.0.0.1:${PORT} (tmux session ${tsess})" >&2
fi
auto_tunnel_stop() { # only for tunnels this run started
  [ "$auto_started" = 1 ] || return 0
  tmux kill-session -t "$tsess" 2>/dev/null || true
  tunnel_children_reap "$PROJECT" "$PORT"
  echo "sql-db: auto-started tunnel stopped (tmux session ${tsess})" >&2
}

# fresh IAM user token as the password (user email URL-encoded: @ -> %40)
# sslmode=require: Cloud SQL Postgres pg_hba rejects unencrypted IAM logins;
# the px IAP tunnel is the trust boundary, so encryption without cert
# verification (require) is sufficient here.
token=$(gcloud auth print-access-token)
user_enc=$(printf '%s' "$USER" | sed 's/@/%40/g')
# Machine interface: emit the real DSN on stdout; human output remains masked.
dsn="postgres://$user_enc@127.0.0.1:$PORT/$DB?sslmode=require"
# display form — the token is replaced, never printed
redacted="postgres://$user_enc@127.0.0.1:$PORT/$DB?sslmode=require"

if [ "$mode" = dsn ]; then
  # stdout stays machine-clean (DSN only); the note goes to stderr. The tunnel
  # is left running — nvim connects through it after fetching the DSN.
  if [ "$auto_started" = 1 ]; then
    echo "sql-db: tunnel left running in tmux session ${tsess} (stop: sql-db.sh --stop --env ${ENV})" >&2
  fi
  printf '%s\n' "$dsn"
  exit 0
fi

echo "sql-db: env=${ENV} project=${PROJECT} instance=${instance}"
echo "sql-db: tunnel 127.0.0.1:$PORT -> $instance (via px IAP)"
echo "sql-db: dsn    $redacted"
echo
echo "nvim:   press ,dD / ,dk (raw DSN: sql-db.sh --env ${ENV} --dsn)"
echo "psql:   sql-db.sh --env ${ENV} --psql"

if [ "$mode" = psql ]; then
  # password via env — never in argv/ps
  pdsn="postgres://$user_enc@127.0.0.1:$PORT/$DB?sslmode=require"
  if [ "$auto_started" = 1 ]; then
    PGPASSWORD="$token" psql "$pdsn"; rc=$?
    auto_tunnel_stop
    exit $rc
  fi
  PGPASSWORD="$token" psql "$pdsn"
  exit $?
fi

if [ "$mode" = rainfrog ]; then
  # rainfrog's only interface is the URL arg (no env support) — token is
  # short-lived (~1h), and the DSN is not printed/logged here.
  if [ "$auto_started" = 1 ]; then
    rainfrog --url "$dsn"; rc=$?
    auto_tunnel_stop
    exit $rc
  fi
  exec rainfrog --url "$dsn"
fi

if [ "$mode" = visql ]; then
  # vi-sql's only interface is --connect (no env support). Pass an explicit
  # name=dsn so vi-sql saves/uses a script-owned connection (<env>-<db>)
  # instead of deriving "<db>" — which collides with any manual connection
  # of that name. Safe-DSN comparison masks the password, so fresh IAM
  # tokens never conflict with the saved entry.
  vname="${ENV}-${DB}"
  if [ "$auto_started" = 1 ]; then
    vi-sql --connect "${vname}=${dsn}"; rc=$?
    auto_tunnel_stop
    exit $rc
  fi
  exec vi-sql --connect "${vname}=${dsn}"
fi