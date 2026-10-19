#!/bin/sh
# service-a-db.sh — compatibility wrapper: default env prod-master, instance/db
# names come from local/projects.env (see scripts/dotfiles-init.sh).
exec "$(dirname "$0")/sql-db.sh" --env prod-master "$@"