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
LOCAL_ENV="${DOTFILES_LOCAL_FILE:-$HOME/dotfiles/local/projects.env}"
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
NAME=""
DB=""
USER=""
PORT="${SQL_DB_PORT:-}"
PROJECT="${SQL_DB_PROJECT:-}"
mode=print
px_extra=""

while [ $# -gt 0 ]; do
  case "$1" in
    --env)      ENV="$2"; shift 2 ;;
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

# ---- stop: PID-file-based teardown -------------------------------------------
if [ "$mode" = stop ]; then
  mkdir -p "$PID_DIR"
  stopped=0; stale=0; skipped=0
  for f in "$PID_DIR"/px-*.pid; do
    [ -e "$f" ] || continue
    tag=$(basename "$f" .pid); pid=$(cat "$f" 2>/dev/null || true)
    rm -f "$f"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      cmd=$(ps -p "$pid" -o command= 2>/dev/null || true)
      case "$cmd" in
        *"px glass"*)
          kill "$pid" 2>/dev/null || true
          echo "sql-db: stopped $tag (pid $pid)"
          stopped=$((stopped + 1)) ;;
        *)
          echo "sql-db: pid $pid ($tag) is NOT a px glass process — left running"
          skipped=$((skipped + 1)) ;;
      esac
    else
      echo "sql-db: stale pidfile $tag (pid ${pid:-?} not running) — removed"
      stale=$((stale + 1))
    fi
  done
  # ports that are open but were never recorded by this script (e.g. started
  # before pidfiles existed, or by another session) — never touch them
  for e in dev-master prod-master prod-eu prod-india; do
    p=$(port_for_env "$e")
    [ -n "$p" ] || continue
    [ -e "$(pidfile_for "$e" "$p")" ] && continue
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

# resolve the Cloud SQL instance live (suffix match, e.g. name~service-a$)
instance=$(gcloud sql instances list --project="$PROJECT" --format="value(name)" \
  | grep -E "${NAME}$" | head -1 || true)
if [ -z "$instance" ]; then
  echo "sql-db: no instance matching '*${NAME}' in ${PROJECT} (env ${ENV})" >&2
  exit 1
fi

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
  mkdir -p "$PID_DIR"
  # atomic claim — two concurrent --fg for the same env can't both win
  if ! ( set -C; echo "$$" > "$pidfile" ) 2>/dev/null; then
    echo "sql-db: tunnel for ${ENV} already starting/running (pidfile $pidfile) — try again or --stop" >&2
    exit 1
  fi
  # token never enters argv or the tunnel; px uses ADC on its own
  echo "sql-db: tunnel ${ENV} (${instance}) on 127.0.0.1:${PORT} — Ctrl-C stops it"
  echo "sql-db: nvim: press ,dD / ,dk (DSN via: sql-db.sh --env ${ENV} --dsn)"
  trap 'rm -f "$pidfile"' INT TERM EXIT
  # shellcheck disable=SC2086
  px glass proxy --backend psql --project "$PROJECT" --local-port "$PORT" $px_extra
  rc=$?
  rm -f "$pidfile"
  exit $rc
fi

# ---- connect modes: tunnel must be up ----------------------------------------
if ! nc -z 127.0.0.1 "$PORT" >/dev/null 2>&1; then
  echo "sql-db: tunnel not up on 127.0.0.1:${PORT} — start it first (this terminal):" >&2
  echo "sql-db:   sql-db.sh --env ${ENV} --fg" >&2
  exit 1
fi

# fresh IAM user token as the password (user email URL-encoded: @ -> %40)
# sslmode=require: Cloud SQL Postgres pg_hba rejects unencrypted IAM logins;
# the px IAP tunnel is the trust boundary, so encryption without cert
# verification (require) is sufficient here.
token=$(gcloud auth print-access-token)
user_enc=$(printf '%s' "$USER" | sed 's/@/%40/g')
dsn="postgres://$user_enc:$token@127.0.0.1:$PORT/$DB?sslmode=require"
# display form — the token is replaced, never printed
redacted="postgres://$user_enc:***@127.0.0.1:$PORT/$DB?sslmode=require"

if [ "$mode" = dsn ]; then
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
  PGPASSWORD="$token" psql "$pdsn"
  exit $?
fi

if [ "$mode" = rainfrog ]; then
  # rainfrog's only interface is the URL arg (no env support) — token is
  # short-lived (~1h), and the DSN is not printed/logged here.
  exec rainfrog --url "$dsn"
fi

if [ "$mode" = visql ]; then
  # vi-sql's only interface is --connect (no env support) — same rationale.
  exec vi-sql --connect "$dsn"
fi