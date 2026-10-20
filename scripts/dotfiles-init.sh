#!/bin/sh
# shellcheck shell=sh disable=SC1090,SC3043,SC2154  # dynamic sources; `local`; eval-indirection for ports
# dotfiles-init.sh — render LOCAL (gitignored) dotfiles from templates.
#
# Real infrastructure names and IDs (herdr projects for each service,
# GCP project IDs, Cloud SQL instance/DB names, tunnel ports, the rainfrog
# connection list) are NOT tracked in this repo. They live in:
#
#     <repo>/local/projects.env        # edit this (see projects.env.example)
#
# and this script renders them into:
#
#     <repo>/local/herdr/{projects,quick-actions,worktrees}/...   (herdr TOMLs)
#     <repo>/local/rainfrog/rainfrog_config.toml                  (DB list)
#
# install.sh symlinks those local dirs into herdr / rainfrog config.
# sql-db.sh and nvim's config/db.lua read <repo>/local/projects.env directly,
# so their commands (sql-db.sh --env X --fg/--visql/--psql/--rainfrog, ,dD)
# stay byte-identical.
#
# Usage:
#   dotfiles-init.sh            validate + render everything
#   dotfiles-init.sh --deps     install DB TUIs (rainfrog, vi-sql) via brew
#                               (cargo/install-script fallback on other OSes)
#   dotfiles-init.sh --example  copy scripts/projects.env.example -> local/
#   dotfiles-init.sh --show     print the local config (passwords never exist)
#   dotfiles-init.sh --help

set -eu

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
LOCALS="$REPO_DIR/local"
ENV_FILE="$LOCALS/projects.env"
TEMPLATES="$REPO_DIR/scripts/templates"

show=""
install_deps=""
example=0
force=0
for arg in "$@"; do
  case "$arg" in
    --deps) install_deps=1 ;;
    --force) force=1 ;;
    --example) example=1 ;;
    --show) show=1 ;;
    --help) sed -n '1,28p' "$0"; exit 0 ;;
    *) echo "dotfiles-init: unknown arg: $arg" >&2; exit 2 ;;
  esac
done

if [ "$example" = 1 ]; then
  if [ -e "$ENV_FILE" ] && [ "$force" != 1 ]; then
    echo "dotfiles-init: $ENV_FILE already exists — NOT overwriting (use --force to replace)" >&2
    exit 1
  fi
  mkdir -p "$LOCALS"
  cp "$REPO_DIR/scripts/projects.env.example" "$ENV_FILE"
  echo "dotfiles-init: created $ENV_FILE from example — edit it, then re-run"
  exit 0
fi

if [ -n "$install_deps" ]; then
  echo "dotfiles-init: installing DB TUIs (rainfrog, vi-sql)..."
  if command -v brew >/dev/null 2>&1; then
    brew install rainfrog vi-sql
  else
    if command -v cargo >/dev/null 2>&1; then
      echo "dotfiles-init: no brew — cargo install rainfrog"
      cargo install rainfrog --locked
    fi
    command -v vi-sql >/dev/null 2>&1 || \
      VI_SQL_VERSION="${VI_SQL_VERSION:-v0.6.0}" sh -c "$(curl -fsSL https://vi-sql.com/install.sh)"
  fi
  command -v rainfrog && command -v vi-sql && echo "dotfiles-init: rainfrog + vi-sql available"
  exit 0
fi

if [ ! -f "$ENV_FILE" ]; then
  echo "dotfiles-init: $ENV_FILE missing." >&2
  echo "  run: dotfiles-init.sh --example   (then edit the values)" >&2
  exit 1
fi
. "$ENV_FILE"

need() {
  eval "v=\${$1:-}"
  [ -n "$v" ] || { echo "dotfiles-init: missing \$$1 in $ENV_FILE" >&2; exit 1; }
}
need DB_USER
need DB_NAME
need KUBECAST_REPO
need KUBECAST_GROUP
need KUBECAST_PROJECTS
for e in dev-master prod-master prod-eu prod-india; do
  k=$(printf '%s' "$e" | tr '-' '_')
  need "env_${k}_project"
  need "env_${k}_port"
done

REPO_BASENAME=$(basename "$KUBECAST_REPO")

render() { # render <template> <out> [service] [env]
  local t="$1" o="$2" svc="${3:-}" envname="${4:-}"
  local d
  d="s|@@REPO@@|${KUBECAST_REPO}|g; s|@@REPO_BASENAME@@|${REPO_BASENAME}|g; s|@@GROUP@@|${KUBECAST_GROUP}|g; s|@@DB_NAME@@|${DB_NAME}|g; s|@@SERVICE@@|${svc}|g; s|@@USER@@|${DB_USER}|g; s|@@ENV@@|${envname}|g"
  mkdir -p "$(dirname "$o")"
  sed -e "$d" "$t" >"$o"
}

# --- herdr projects / quick-actions / worktrees ---
herdr_out="$LOCALS/herdr"
for svc in $KUBECAST_PROJECTS; do
  render "$TEMPLATES/herdr-project.toml"        "$herdr_out/projects/$svc.toml" "$svc"
  render "$TEMPLATES/herdr-project-action.toml" "$herdr_out/quick-actions/$svc-project.toml" "$svc"
  render "$TEMPLATES/herdr-worktree-action.toml" "$herdr_out/quick-actions/$svc-worktree.toml" "$svc"
  render "$TEMPLATES/herdr-worktree-service.toml" "$herdr_out/worktrees/$REPO_BASENAME-$svc.toml" "$svc"
done
render "$TEMPLATES/herdr-project-root.toml" "$herdr_out/projects/$REPO_BASENAME.toml"
render "$TEMPLATES/herdr-root-action.toml" "$herdr_out/quick-actions/$REPO_BASENAME-project.toml"
render "$TEMPLATES/herdr-repo-worktree.toml" "$herdr_out/worktrees/$REPO_BASENAME.toml"

# --- DB workspaces: one project + quick action per env (kent) ---
for envname in dev-master prod-master prod-eu prod-india; do
  render "$TEMPLATES/herdr-db-project.toml" "$herdr_out/projects/db-$envname.toml" "" "$envname"
  render "$TEMPLATES/herdr-db-action.toml" "$herdr_out/quick-actions/db-$envname.toml" "" "$envname"
done

# --- rainfrog: base settings + [db] per env ---
rainfrog_out="$herdr_out/../rainfrog/rainfrog_config.toml"
mkdir -p "$(dirname "$rainfrog_out")"
{
  cat <<'BASE'
# rainfrog config — rendered locally by scripts/dotfiles-init.sh (NOT tracked).
# Connections point at the per-env px tunnel ports; IAM tokens rotate hourly,
# so passwords are never stored here. Primary flow: sql-db.sh --env <e> --rainfrog.
mouse_mode = true

[db]
BASE
  for e in dev-master prod-master prod-eu prod-india; do
    k=$(printf '%s' "$e" | tr '-' '_')
    eval "p=\${env_${k}_port}"
    printf '%s = { host = "127.0.0.1", driver = "postgres", port = %s, database = "%s", username = "%s" }\n' \
      "$e" "$p" "$DB_NAME" "$DB_USER"
  done
} >"$rainfrog_out"

echo "dotfiles-init: OK — rendered:"
echo "  herdr: $herdr_out/{projects,quick-actions,worktrees}"
echo "  rainfrog: $rainfrog_out"
echo "next: install.sh symlinks these into place (rerun install -i herdr / -i rainfrog if needed)"

if [ -n "$show" ]; then
  echo
  echo "--- $ENV_FILE ---"
  sed -E 's/^([A-Za-z0-9_]+)=.*/\1=<set>/' "$ENV_FILE"
fi