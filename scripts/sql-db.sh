#!/bin/sh
# sql-db.sh — connect to a CAST AI Cloud SQL Postgres instance from nvim/psql,
# any environment, without cloud-sql-proxy and without stored passwords.
#
#   Tunnel:  CAST AI CLI `px glass proxy` — IMPORTANT: this is an interactive
#            TUI wizard (bubbletea) and REQUIRES a real terminal. Start it with
#            `--fg` in a terminal and leave it running; Ctrl-C stops it.
#            Needs GCP ADC (gcloud auth application-default login); PAM grants
#            are assumed active (--skip-grants if already elevated, or run
#            `px glass break` first).
#   Auth:    Cloud SQL IAM — the short-lived gcloud token is used as the
#            password, minted fresh per run. Read-only by default; elevate
#            to write with `px glass database` (breakglass_readwrite, windowed).
#
# Usage:
#   sql-db.sh --env dev-master --fg   start the tunnel in THIS terminal
#                                     (wizard; Ctrl-C stops it; run once per env)
#   sql-db.sh [--env X] [--name N]    tunnel must be up; prints the fresh
#                                     IAM DSN for nvim (:DB ...)
#   sql-db.sh --env X --psql          same, then exec psql
#   sql-db.sh --env X --rainfrog      same, then open rainfrog TUI with the
#                                     fresh DSN (spreadsheet-style browser)
#   sql-db.sh --stop                  kill px glass processes
#   extra flags pass through to px glass proxy (--instance VM, --zone Z,
#                                     --skip-grants, ...)
#
# Defaults: --env prod-master --name service-a --port 15432
# Overrides via env: SQL_DB_PROJECT, SQL_DB_USER, SQL_DB_PORT.

set -eu

# env -> GCP project (verified 2026-10: instance + IAM user exist per env)
project_for_env() {
  case "$1" in
    prod-master) echo "project-prod-master" ;;
    prod-eu)     echo "project-prod-eu" ;;
    prod-india)  echo "project-prod-india" ;;
    dev-master)  echo "project-dev-master" ;;
    *) echo "sql-db: unknown env '$1' (prod-master|prod-eu|prod-india|dev-master)" >&2; exit 2 ;;
  esac
}

# env -> local tunnel port (keep in sync with nvim/lua/config/db.lua ENV_PORTS)
port_for_env() {
  case "$1" in
    dev-master)  echo "15432" ;;
    prod-master) echo "15433" ;;
    prod-eu)     echo "15434" ;;
    prod-india)  echo "15435" ;;
    *) echo "15432" ;;
  esac
}

ENV="prod-master"
NAME="service-a"
DB=""
USER="marcin@cast.ai"
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
    --psql)     mode=psql; shift ;;
    --rainfrog) mode=rainfrog; shift ;;
    --stop)     mode=stop; shift ;;
    --*)        px_extra="$px_extra $1"; shift ;;
    *) echo "sql-db: unknown arg: $1" >&2; exit 2 ;;
  esac
done

[ -n "$PROJECT" ] || PROJECT=$(project_for_env "$ENV")
[ -n "$PORT" ] || PORT=$(port_for_env "$ENV")
[ -n "$DB" ] || DB="$NAME"

if [ "$mode" = stop ]; then
  pkill -f "px glass" 2>/dev/null || true
  echo "sql-db: px tunnels stopped"
  exit 0
fi

# px needs Application Default Credentials
if ! gcloud auth application-default print-access-token >/dev/null 2>&1; then
  echo "sql-db: GCP ADC missing — run: gcloud auth application-default login" >&2
  exit 1
fi

# resolve the Cloud SQL instance live (suffix match, e.g. name~service-a$)
instance=$(gcloud sql instances list --project="$PROJECT" --format="value(name)" \
  | grep -E "${NAME}$" | head -1 || true)
if [ -z "$instance" ]; then
  echo "sql-db: no instance matching '*${NAME}' in ${PROJECT}" >&2
  exit 1
fi

# fresh IAM token as the password (user email URL-encoded: @ -> %40)
# sslmode=require: Cloud SQL Postgres pg_hba rejects unencrypted IAM logins;
# the px IAP tunnel is the trust boundary, so encryption without cert
# verification (require) is sufficient here.
token=$(gcloud auth print-access-token)
user_enc=$(printf '%s' "$USER" | sed 's/@/%40/g')
dsn="postgres://$user_enc:$token@127.0.0.1:$PORT/$DB?sslmode=require"

if [ "$mode" = foreground ]; then
  echo "sql-db: tunnel ${ENV} (${instance}) on 127.0.0.1:${PORT} — Ctrl-C stops it"
  echo "sql-db: dsn    $dsn"
  echo "sql-db: (in nvim: press ,dD / ,dk, or :DB <dsn>)"
  # shellcheck disable=SC2086
  exec px glass proxy --backend psql --project "$PROJECT" --local-port "$PORT" $px_extra
fi

if ! nc -z 127.0.0.1 "$PORT" >/dev/null 2>&1; then
  echo "sql-db: tunnel not up on 127.0.0.1:${PORT} — start it first (this terminal):" >&2
  echo "sql-db:   sql-db.sh --env ${ENV} --fg" >&2
  exit 1
fi

echo "sql-db: env=${ENV} project=${PROJECT} instance=${instance}"
echo "sql-db: tunnel 127.0.0.1:$PORT -> $instance (via px IAP)"
echo "sql-db: dsn    $dsn"
echo
echo "nvim:   :DB $dsn"
echo "psql:   psql \"$dsn\""

if [ "$mode" = psql ]; then
  exec psql "$dsn"
fi

if [ "$mode" = rainfrog ]; then
  exec rainfrog --url "$dsn"
fi