#!/bin/sh
# service-a-db.sh — connect to the service-a Cloud SQL instance without cloud-sql-proxy.
#
# Tunnel:  CAST AI CLI IAP tunnel (px glass proxy --backend psql).
#          Requires GCP ADC (gcloud auth application-default login) and
#          active PAM grants — add --skip-grants if already elevated, or run
#          `px glass break` / `px glass database` first for write access.
#          If px prompts for a host, pick the GCE instance that hosts the DB.
# Auth:    Cloud SQL IAM — user marcin@cast.ai already exists (CLOUD_IAM_USER);
#          the short-lived gcloud token is used as the password, per session.
#          Users are read-only by default; `px glass database` elevates to
#          write (breakglass_readwrite) for a limited window.
#
# Usage:
#   service-a-db.sh              ensure tunnel up, print DSN + psql one-liner
#   service-a-db.sh --psql       run psql directly (needs psql on PATH)
#   service-a-db.sh --stop       stop the background tunnel
#   service-a-db.sh --skip-grants ...   pass extra flags through to `px glass proxy`
#
# Overrides: KENT_DB_PORT, KENT_DB_NAME, KENT_DB_USER, KENT_DB_PROJECT env vars.

set -eu

INSTANCE="sql-prod-master00000000-service-a"
PROJECT="${KENT_DB_PROJECT:-project-prod-master}"
LOCAL_PORT="${KENT_DB_PORT:-15432}"
DB_NAME="${KENT_DB_NAME:-service-a}"
DB_USER="${KENT_DB_USER:-marcin@cast.ai}"

mode=print
px_extra=""
for arg in "$@"; do
  case "$arg" in
    --psql) mode=psql ;;
    --stop) mode=stop ;;
    --*)    px_extra="$px_extra $arg" ;;
    *) echo "service-a-db: unknown arg: $arg" >&2; exit 2 ;;
  esac
done

log_file="${TMPDIR:-/tmp}/service-a-db-px.log"

if [ "$mode" = stop ]; then
  # stop only the tunnel we started (px glass proxy)
  pkill -f "px glass proxy" 2>/dev/null || true
  echo "tunnel stopped"
  exit 0
fi

# 1. px needs Application Default Credentials
if ! gcloud auth application-default print-access-token >/dev/null 2>&1; then
  echo "service-a-db: GCP ADC missing — run: gcloud auth application-default login" >&2
  exit 1
fi

# 2. start the IAP tunnel if the local port is not yet open
if ! nc -z 127.0.0.1 "$LOCAL_PORT" >/dev/null 2>&1; then
  echo "service-a-db: starting IAP tunnel (px glass proxy --backend psql --local-port $LOCAL_PORT)$px_extra"
  # shellcheck disable=SC2086
  nohup px glass proxy --backend psql --project "$PROJECT" --local-port "$LOCAL_PORT" \
    $px_extra >"$log_file" 2>&1 &
  i=0
  while [ "$i" -lt 45 ] && ! nc -z 127.0.0.1 "$LOCAL_PORT" >/dev/null 2>&1; do
    sleep 1
    i=$((i + 1))
  done
  if ! nc -z 127.0.0.1 "$LOCAL_PORT" >/dev/null 2>&1; then
    echo "service-a-db: tunnel did not come up — see $log_file" >&2
    exit 1
  fi
fi

# 3. mint a fresh IAM token as the password and print the connection
token=$(gcloud auth print-access-token)
dsn="postgres://$DB_USER:$token@127.0.0.1:$LOCAL_PORT/$DB_NAME?sslmode=disable"
echo "service-a-db: tunnel 127.0.0.1:$LOCAL_PORT -> $INSTANCE (via px IAP)"
echo "service-a-db: dsn    $dsn"
echo
echo "nvim:   :DB $dsn"
echo "psql:   psql \"$dsn\""

if [ "$mode" = psql ]; then
  exec psql "$dsn"
fi