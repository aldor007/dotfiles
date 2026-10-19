#!/bin/sh
# service-a-db.sh — compatibility wrapper: default env prod-master, instance service-a.
# Use scripts/sql-db.sh for all environments.
exec "$(dirname "$0")/sql-db.sh" --env prod-master --name service-a "$@"