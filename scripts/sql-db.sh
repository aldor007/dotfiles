#!/bin/sh
# sql-db.sh — connect to a CAST AI Cloud SQL Postgres instance from nvim/psql,
# any environment, without cloud-sql-proxy and without stored passwords.
#
#   Tunnel:  CAST AI CLI IAP tunnel (px glass proxy --backend psql), started on
#            demand in the background; needs GCP ADC (gcloud auth
#            application-default login) + active PAM grants (--skip-grants if
#            already elevated, or `px glass break` first). If px prompts for a
#            host, pick the GCE instance that hosts the DB.
#   Auth:    Cloud SQL IAM — the short-lived gcloud token is used as the
#            password, minted fresh per run. Read-only by default; elevate
#            to write with `px glass database` (breakglass_readwrite, windowed).
#
# Usage:
#   sql-db.sh [--env prod-master|prod-eu|prod-india|dev-master]
#             [--name service-a|service-b|service-repo|cost|...]   (suffix match)
#             [--db NAME] [--user EMAIL] [--port N] [--project ID]
#             [--psql] [--stop] [extra px flags e.g. --skip-grants]
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
    --env)   ENV="$2"; shift 2 ;;
    --name)  NAME="$2"; shift 2 ;;
    --db)    DB="$2"; shift 2 ;;
    --user)  USER="$2"; shift 2 ;;
    --port)  PORT="$2"; shift 2 ;;
    --project) PROJECT="$2"; shift 2 ;;
    --psql)  mode=psql; shift ;;
    --stop)  mode=stop; shift ;;
    --*)     px_extra="$px_extra $1"; shift ;;
    *) echo "sql-db: unknown arg: $1" >&2; exit 2 ;;
  esac
done

[ -n "$PROJECT" ] || PROJECT=$(project_for_env "$ENV")
[ -n "$PORT" ] || PORT=$(port_for_env "$ENV")
[ -n "$DB" ] || DB="$NAME"
log_file="${TMPDIR:-/tmp}/sql-db-px.log"

if [ "$mode" = stop ]; then
  pkill -f "px glass proxy" 2>/dev/null || true
  echo "sql-db: tunnel stopped"
  exit 0
fi

# 1. px needs Application Default Credentials
if ! gcloud auth application-default print-access-token >/dev/null 2>&1; then
  echo "sql-db: GCP ADC missing — run: gcloud auth application-default login" >&2
  exit 1
fi

# 2. resolve the Cloud SQL instance live (suffix match, e.g. name~service-a$)
instance=$(gcloud sql instances list --project="$PROJECT" --format="value(name)" \
  | grep -E "${NAME}$" | head -1 || true)
if [ -z "$instance" ]; then
  echo "sql-db: no instance matching '*${NAME}' in ${PROJECT}" >&2
  exit 1
fi

# 3. start the IAP tunnel if the local port is not yet open
if ! nc -z 127.0.0.1 "$PORT" >/dev/null 2>&1; then
  echo "sql-db: starting IAP tunnel (px glass proxy --backend psql --local-port $PORT)$px_extra"
  # shellcheck disable=SC2086
  nohup px glass proxy --backend psql --project "$PROJECT" --local-port "$PORT" \
    $px_extra >"$log_file" 2>&1 &
  i=0
  while [ "$i" -lt 45 ] && ! nc -z 127.0.0.1 "$PORT" >/dev/null 2>&1; do
    sleep 1
    i=$((i + 1))
  done
  if ! nc -z 127.0.0.1 "$PORT" >/dev/null 2>&1; then
    echo "sql-db: tunnel did not come up — see $log_file" >&2
    exit 1
  fi
fi

# 4. mint a fresh IAM token as the password and print the connection
#    (user email must be URL-encoded: @ -> %40)
token=$(gcloud auth print-access-token)
user_enc=$(printf '%s' "$USER" | sed 's/@/%40/g')
dsn="postgres://$user_enc:$token@127.0.0.1:$PORT/$DB?sslmode=disable"
echo "sql-db: env=${ENV} project=${PROJECT} instance=${instance}"
echo "sql-db: tunnel 127.0.0.1:$PORT -> $instance (via px IAP)"
echo "sql-db: dsn    $dsn"
echo
echo "nvim:   :DB $dsn"
echo "psql:   psql \"$dsn\""

if [ "$mode" = psql ]; then
  exec psql "$dsn"
fi