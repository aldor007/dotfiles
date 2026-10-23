#!/bin/bash
# dotfiles installer — idempotent, non-destructive by default.
#
# Safety model:
#   * Nothing existing is ever deleted: before a config file/dir is replaced
#     with a symlink into this repo, the original is moved to a timestamped
#     backup under ${DOTFILES_BACKUP_DIR:-~/.dotfiles-backup}.
#   * Destructive operations (the -c/--clean mode, which clears the home-dir
#     config paths BEFORE re-linking) require an explicit `--force`.
#   * --dry-run prints every command that would run, without executing it.
#   * Re-runs are idempotent: links already pointing at this repo are kept,
#     clones are skipped when the target dir exists, tool installs are
#     skipped when the tool is already present where possible.
#
# Usage:
#   install.sh [-h] [-d|--dry-run] [-f|--force] [-i|--install <target>]
#              [-c|--clean]
#
#   -i, --install <target>   run one install target (see list below)
#   -c, --clean              before linking, move existing home-dir configs
#                            to backup (REQUIRES --force; moves, never deletes)
#   -d, --dry-run            print what would run; change nothing
#   -f, --force              allow destructive pre-clean
#   -h, --help               this text
#
# Targets: herdr nvim vim zsh tmux fzf repo formatters rainfrog vi-sql
#          fonts bin programing all
set -euo pipefail

# The repo is wherever this script lives — no hardcoded ~/dotfiles.
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$REPO_DIR/"
BACKUP_ROOT="${DOTFILES_BACKUP_DIR:-$HOME/.dotfiles-backup}"
DRY=0
FORCE=0
install_typ=""

usage() { sed -n '1,30p' "$0"; }

run() { # run <cmd...> — executes, or prints under --dry-run
  if [ "$DRY" = 1 ]; then
    printf '[dry-run] %s\n' "$*"
  else
    "$@"
  fi
}

backup_now() { # backup_now <path> — collision-resistant move into BACKUP_ROOT
  local p="$1" ts dst
  [ -e "$p" ] || [ -L "$p" ] || return 0
  if [ "$DRY" = 1 ]; then
    echo "install: [dry-run] would back up $p into $BACKUP_ROOT/"
    return
  fi
  ts=$(date +%Y%m%d-%H%M%S)
  mkdir -p "$BACKUP_ROOT"
  # mktemp -d guarantees a unique target even for same-second re-runs —
  # existing backups are never overwritten
  dst=$(mktemp -d "$BACKUP_ROOT/$ts.XXXXXX") || { echo "install: backup failed for $p" >&2; return 1; }
  echo "install: backing up $p -> $dst/$(basename "$p")"
  run mv "$p" "$dst/"
}

# link_or_backup <target> <link> — idempotent symlink with backup of anything
# that currently occupies <link> and is NOT already a symlink to <target>.
link_or_backup() {
  local target="$1" link="$2"
  if [ -L "$link" ]; then
    local cur
    cur=$(readlink "$link" 2>/dev/null || true)
    [ "$cur" = "$target" ] && return 0 # already correct — never re-backup
    echo "install: $link points to $cur (not $target) — preserving it via backup"
    backup_now "$link"
  elif [ -e "$link" ]; then
    backup_now "$link"
  fi
  local parent
  parent=$(dirname "$link")
  [ -d "$parent" ] || run mkdir -p "$parent"
  run ln -s "$target" "$link"
}

function install_common {
    command -v apt-get >/dev/null 2>&1 || { echo "install_common: apt-get not found — skipping (Debian/Ubuntu only)"; return; }
    run sudo apt-get install git -y
    run sudo apt-get install zip -y
    run sudo apt-get install unzip -y
    run sudo apt-get install subversion -y
    run sudo apt-get install mercurial -y
    run sudo apt-get install build-essential -y
    run sudo apt-get install python-pip -y
    run sudo apt-get install screen -y
    link_or_backup "${BASE_DIR}.screenrc" "$HOME/.screenrc"
}

function install_fonts {
  command -v apt-get >/dev/null 2>&1 || { echo "install_fonts: skipping (Debian/Ubuntu only)"; return; }
  if [ "$DRY" = 1 ]; then
    echo "[dry-run] install_fonts: would download powerline fonts into ~/.fonts + ~/.config/fontconfig"
    return
  fi
  local td
  td=$(mktemp -d)
  ( cd "$td"
    run curl -fLO https://github.com/Lokaltog/powerline/raw/develop/font/PowerlineSymbols.otf
    run curl -fLO https://github.com/Lokaltog/powerline/raw/develop/font/10-powerline-symbols.conf
    run mkdir -p "$HOME/.fonts" "$HOME/.config/fontconfig/conf.d"
    run mv -f PowerlineSymbols.otf "$HOME/.fonts/"
    run mv -f 10-powerline-symbols.conf "$HOME/.config/fontconfig/conf.d/"
    run fc-cache -vf "$HOME/.fonts/"
  )
  run rm -rf "$td"
}

function install_zsh {
    command -v apt-get >/dev/null 2>&1 || { echo "install_zsh: skipping (Debian/Ubuntu only)"; return; }
    run sudo apt-get install zsh -y
    run sudo chsh -s /bin/zsh "${USER:-$(whoami)}"
    [ -d "$HOME/.oh-my-zsh" ] || run git clone --depth 1 https://github.com/robbyrussell/oh-my-zsh "$HOME/.oh-my-zsh"
    link_or_backup "${BASE_DIR}.zshrc" "$HOME/.zshrc"
    [ -d "$HOME/.oh-my-zsh/plugins/zsh-autosuggestions" ] || \
      run git clone https://github.com/zsh-users/zsh-autosuggestions "$HOME/.oh-my-zsh/plugins/zsh-autosuggestions"
    run mkdir -p "$HOME/.oh-my-zsh/themes"
    link_or_backup "${BASE_DIR}zsh-themes/birav1.zsh-theme" "$HOME/.oh-my-zsh/themes/birav1.zsh-theme"
}

function install_tmux {
    command -v apt-get >/dev/null 2>&1 || { echo "install_tmux: skipping (Debian/Ubuntu only)"; return; }
    run sudo apt-get install python-fontforge -y
    if cat /etc/issue 2>/dev/null | grep -qi ubuntu; then
        run sudo apt-get install -y python-software-properties software-properties-common
        run sudo add-apt-repository -y ppa:pi-rho/dev
        run sudo apt-get update
    fi
    run sudo apt-get install tmux -y
    run sudo apt-get install python-netifaces -y
    link_or_backup "${BASE_DIR}.tmux" "$HOME/.tmux"
    link_or_backup "${BASE_DIR}.config/powerline/" "$HOME/.config/powerline"
    [ -d "$HOME/.tmux/powerline2" ] || run git clone https://github.com/Lokaltog/powerline "$HOME/.tmux/powerline2"
    if [ -d "$HOME/.tmux/powerline2" ]; then
      ( cd "$HOME/.tmux/powerline2" && run sudo python setup.py install )
    fi
    link_or_backup "${BASE_DIR}.tmux/tmux.conf" "$HOME/.tmux.conf"
}

function install_programing {
    command -v apt-get >/dev/null 2>&1 && {
        run sudo apt-get install python -y || true
        run sudo apt-get install python-pip -y || true
    }
    [ -d "$HOME/.nvm" ] || run bash -c 'curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.1/install.sh | bash'

    export NVM_DIR="$HOME/.nvm"
    [ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"  # This loads nvm
    run nvm install stable
    run nvm use stable
    run npm install -g jslint
    run npm install -g eslint
    run mkdir -p "$HOME/.bin"
    if [ "$(uname -s)" = "Linux" ]; then
        run bash -c 'cd "$HOME/.bin" && curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && chmod a+x kubectl'
    else
        echo "kubectl: skipping (linux/amd64 binary only; use brew install kubectl instead)"
    fi
    [ -x "$HOME/.bin/helm" ] || run bash -c 'curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash'
    run bash -c 'curl -sL https://istio.io/downloadIstioctl | sh -'
    [ -d "$HOME/.tgenv" ] || run git clone https://github.com/cunymatthieu/tgenv.git "$HOME/.tgenv"
    if [ "$(uname -s)" = "Linux" ]; then
        run bash -c 'curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash'
        run bash -c 'cd /tmp && curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o awscliv2.zip && unzip -o awscliv2.zip && sudo ./aws/install'
    else
        echo "awscli/azure: skipping (Linux installers; use brew install awscli azure-cli)"
    fi
    [ -d "$HOME/.tfenv" ] || run git clone https://github.com/tfutils/tfenv.git "$HOME/.tfenv"
    # krew (repeatable, idempotent block)
    [ -x "$HOME/.krew/bin/kubectl-krew" ] || run bash -c '
  cd "$(mktemp -d)" &&
  OS="$(uname | tr "[:upper:]" "[:lower:]")" &&
  ARCH="$(uname -m | sed -e "s/x86_64/amd64/" -e "s/\(arm\)\(64\)\?.*/\1\2/" -e "s/aarch64$/arm64/")" &&
  KREW="krew-${OS}_${ARCH}" &&
  curl -fsSLO "https://github.com/kubernetes-sigs/krew/releases/latest/download/${KREW}.tar.gz" &&
  tar zxvf "${KREW}.tar.gz" &&
  ./"${KREW}" install krew'
}

function install_vim {
    command -v apt-get >/dev/null 2>&1 || { echo "install_vim: skipping (Debian/Ubuntu only)"; return; }
    run sudo apt-get install fontforge --yes
    run sudo apt-get install vim-nox --yes
    run sudo apt-get install ctags --yes
    run sudo pip install jedi || true
    run sudo pip install pylint || true
    link_or_backup "${BASE_DIR}.jshintrc" "$HOME/.jshintrc"
    link_or_backup "${BASE_DIR}.eslintrc.json" "$HOME/.eslintrc.json"
    link_or_backup "${BASE_DIR}.vim" "$HOME/.vim"
    [ -d "$HOME/.vim/bundle/Vundle.vim" ] || run git clone https://github.com/gmarik/Vundle.vim "$HOME/.vim/bundle/Vundle.vim"
    link_or_backup "${BASE_DIR}.pylintrc" "$HOME/.pylintrc"
    link_or_backup "${BASE_DIR}.vimrc" "$HOME/.vimrc"
    [ "$DRY" = 1 ] || echo | vim -c 'BundleInstall' -c qa
}

# nvim: link the repo config in and make sure a recent nvim exists.
function install_nvim {
    link_or_backup "$REPO_DIR/nvim" "$HOME/.config/nvim"
    if ! command -v nvim >/dev/null 2>&1; then
        if command -v brew >/dev/null 2>&1; then
            run brew install nvim
        else
            echo "install_nvim: nvim not found — install it (brew install nvim) then re-run"
        fi
    else
        # nvim --version creates its state dir — never probe it under --dry-run
        if [ "$DRY" = 1 ]; then
            echo "install_nvim: [dry-run] nvim present — config would be linked + printed version/validation"
        else
            echo "install_nvim: nvim present ($(nvim --version | head -1)) — config linked to $HOME/.config/nvim"
        fi
    fi
    echo "install_nvim: validate with: $REPO_DIR/nvim/scripts/validate.sh"
}

function install_fzf {
    if [ -d "$HOME/.fzf" ]; then
        # upgrade-aware: pull the existing clone, then re-run the installer
        ( cd "$HOME/.fzf" && run git pull --ff-only )
    else
        run git clone --depth 1 https://github.com/junegunn/fzf.git "$HOME/.fzf"
    fi
    run "$HOME/.fzf/install" --all </dev/null
}

function install_gitconf {
    command -v apt-get >/dev/null 2>&1 || { echo "install_gitconf: skipping (Debian/Ubuntu only)"; return; }
    run sudo apt-get install libncurses5-dev libncursesw5-dev -y
    link_or_backup "${BASE_DIR}.gitconfig" "$HOME/.gitconfig"
    [ -d /tmp/tig/.git ] || run git clone https://github.com/jonas/tig /tmp/tig
    if [ -d /tmp/tig/.git ]; then
      ( cd /tmp/tig && run sudo make prefix=/usr/local && run sudo make install prefix=/usr/local )
    fi
}

function install_bin {
    run mkdir -p "$HOME/.bin/"
    run cp -R "$REPO_DIR/scripts/." "$HOME/.bin/"
}

# fd: fast, friendly file finder — used by telescope (find_files/<space>g)
# and fzf-lua-style workflows. macOS: brew. Debian/Ubuntu: fd-find package
# installs the binary as `fdfind`; cargo fallback gives `fd` everywhere.
function install_fd {
    if command -v brew >/dev/null 2>&1; then
        if command -v fd >/dev/null 2>&1; then
            echo "install_fd: fd present ($(fd --version | head -1)) — skipping"
        else
            run brew install fd
        fi
    elif command -v apt-get >/dev/null 2>&1; then
        run sudo apt-get install -y fd-find
        echo "install_fd: Debian installs fd-find — binary name is fdfind (alias fd=fdfind if needed)"
    elif command -v cargo >/dev/null 2>&1; then
        run cargo install fd-find --locked
    else
        echo "install_fd: no brew/apt/cargo found — install manually (brew install fd)"
    fi
}

# Formatters/linters used by conform.nvim + nvim-lint (nvim/lua/plugins/format.lua).
function install_formatters {
    if command -v brew >/dev/null 2>&1; then
        run brew install stylua ruff prettier shellcheck
        echo "formatters installed: stylua ruff prettier shellcheck"
    elif command -v apt-get >/dev/null 2>&1; then
        run sudo apt-get install -y shellcheck
        echo "apt: shellcheck installed; install stylua (cargo), ruff (pipx), prettier (npm) separately"
    else
        echo "install_formatters: no brew/apt found — install stylua, ruff, prettier, shellcheck manually"
    fi
}

# vi-sql: vim-mode SQL TUI (schema browser, grid editing, EXPLAIN, MCP).
function install_vi_sql {
    if command -v brew >/dev/null 2>&1; then
        command -v vi-sql >/dev/null 2>&1 || run brew install vi-sql
    else
        command -v vi-sql >/dev/null 2>&1 || run bash -c 'VI_SQL_VERSION="${VI_SQL_VERSION:-v0.6.0}" sh -c "$(curl -fsSL https://vi-sql.com/install.sh)"'
    fi
    echo "vi-sql installed: vi-sql --connect <dsn> (or sql-db.sh --visql)"
}

# rainfrog: spreadsheet-style DB TUI. Config is symlinked — LOCAL generated
# config (dotfiles-init.sh) wins; tracked base is the fallback.
function install_rainfrog {
    if command -v brew >/dev/null 2>&1; then
        command -v rainfrog >/dev/null 2>&1 || run brew install rainfrog
    elif command -v cargo >/dev/null 2>&1; then
        command -v rainfrog >/dev/null 2>&1 || run cargo install rainfrog --locked
    else
        echo "install_rainfrog: no brew/cargo found — install manually (brew install rainfrog)"
        return
    fi
    local conf
    case "$(uname -s)" in
        Darwin) conf="$HOME/Library/Application Support/dev.rainfrog.rainfrog" ;;
        *)      conf="$HOME/.config/rainfrog" ;;
    esac
    local src="$REPO_DIR/local/rainfrog/rainfrog_config.toml"
    [ -f "$src" ] || src="$REPO_DIR/.config/rainfrog/rainfrog_config.toml"
    link_or_backup "$src" "$conf/rainfrog_config.toml"
    echo "rainfrog installed; config linked -> $conf/rainfrog_config.toml"
}

# target -> the home-dir paths the target manages (what --clean backs up).
# clean NEVER touches anything outside this mapping.
path_map() { # path_map <target>
  case "$1" in
    herdr) echo "$HOME/.config/herdr" ;;
    nvim)  echo "$HOME/.config/nvim" ;;
    zsh)   echo "$HOME/.zshrc"; echo "$HOME/.oh-my-zsh" ;;
    tmux)  echo "$HOME/.tmux"; echo "$HOME/.tmux.conf"; echo "$HOME/.screenrc" ;;
    vim)   echo "$HOME/.vim"; echo "$HOME/.vimrc"; echo "$HOME/.jshintrc"; \
           echo "$HOME/.eslintrc.json"; echo "$HOME/.pylintrc" ;;
    all)   for t in herdr nvim zsh tmux vim; do path_map "$t"; done ;;
    *)     return 1 ;;
  esac
}

# clean: back up ONLY the paths belonging to the selected target, so
# `-c -i nvim` never unplugs zsh/tmux/herdr configs. Moves only, never deletes.
clean_install() { # clean_install <target>
  local p paths
  paths=$(path_map "$1") || {
    echo "install: cannot clean unknown target '$1'" >&2; return 1
  }
  echo "Clean install for '$1' (backing up to $BACKUP_ROOT)"
  for p in $paths; do
    # a link already pointing into this repo is the install-managed link: the
    # install target re-creates it byte-identical, so backing it up would grow
    # the backup tree on every repeat clean for nothing
    if [ -L "$p" ]; then
      case "$(readlink "$p" 2>/dev/null || true)" in
        "$REPO_DIR"/*) echo "install: $p is an install-managed link — skipping backup"; continue ;;
      esac
    fi
    backup_now "$p"
  done
}

# herdr: install the CLI + plugins; plugin config dirs are symlinked into the
# repo (or into local/ for generated configs).
function install_herdr {
    command -v herdr >/dev/null 2>&1 || run bash -c 'curl -fsSL https://herdr.dev/install.sh | sh'
    run herdr plugin install --yes cloudmanic/herdr-plus
    run herdr plugin install --yes speardragon/herdr-command-center
    run herdr plugin install --yes ChmaraX/herdr-nvim
    run herdr plugin install --yes Davidcreador/herdr-token-dashboard
    run herdr plugin install --yes jpwallace22/herdr-glab-status
    run herdr plugin install --yes a2u/herdr-jira
    run herdr plugin install --yes robbyrussell/herdr-ohmyzsh
    run herdr plugin install --yes persiyanov/herdr-reviewr
    # herdr-bar: fuzzy jump (prefix+space binding in config.toml)
    run herdr plugin install --yes jeffarese/herdr-bar
    # herdr-projects: multi-agent board (prefix+o binding in config.toml).
    # Configured to run pi (= the kimchi shim on ~/.local/bin/pi) for
    # worker/reviewer/coordinator/watcher — settings live locally in
    # ~/.herdr-projects/settings/config.json.
    run herdr plugin install --yes eliasstravik/herdr-projects
    # kimchi-remote: local-only plugin (not on github — verified via
    # `curl https://github.com/castai/kimchi-remote` → 404). Lives under
    # local/herdr-kimchi-remote/ and gets installed by path so the
    # prefix+w / prefix+shift+w / prefix+alt+w / prefix+a bindings in
    # config.toml resolve. Audit finding P1 (plugin listed but never
    # installed). Falls back to a CLEAR WARNING (not a silent skip) when
    # the local source is absent — the previous silent skip left a clean
    # install reporting success while four keybindings pointed at a
    # missing plugin. The validate step below proves the install actually
    # took effect.
    # kimchi-remote: local-only plugin (not on github — verified via
    # `curl https://github.com/castai/kimchi-remote` → 404). Lives under
    # local/herdr-kimchi-remote/ and must be linked in from the repo so the
    # prefix+w / prefix+shift+w / prefix+alt+w / prefix+a bindings in
    # config.toml resolve. Prefer `herdr plugin link`, which is the current
    # local-plugin registration path; fallback to the legacy path-based
    # install only when the installed herdr does not support `link`.
    local_kimchi_dir="$REPO_DIR/local/herdr-kimchi-remote"
    if [ -d "$local_kimchi_dir" ]; then
        if herdr plugin link --help >/dev/null 2>&1; then
            if ! herdr plugin list 2>/dev/null | grep -q "kimchi-remote"; then
                if ! herdr plugin link "$local_kimchi_dir"; then
                    echo "install_herdr: ERROR failed to link kimchi-remote from $local_kimchi_dir" >&2
                    echo "install_herdr:          check the plugin manifest and retry; no local plugin cleanup was attempted." >&2
                fi
            else
                echo "install_herdr: kimchi-remote already linked; preserving existing installation"
            fi
        else
            if ! herdr plugin install --yes --path "$local_kimchi_dir"; then
                echo "install_herdr: ERROR failed to install kimchi-remote from $local_kimchi_dir" >&2
                echo "install_herdr:          plugin was not silently ignored; fix the local plugin before retrying." >&2
            fi
        fi
        # Validate the install actually landed — a clean clone should not
        # report successful setup while configured shortcuts are unresolved.
        if herdr plugin list 2>/dev/null | grep -q "kimchi-remote"; then
            echo "install_herdr: kimchi-remote installed and discoverable"
            if command -v python3 >/dev/null 2>&1; then
                action_list_json=$(herdr plugin action list 2>&1)
                rc=$?
                if [ "$rc" -ne 0 ]; then
                    echo "install_herdr: WARNING unable to discover kimchi-remote actions via 'herdr plugin action list':" >&2
                    echo "$action_list_json" >&2
                elif [ -z "$action_list_json" ]; then
                    echo "install_herdr: WARNING kimchi-remote action discovery returned empty output" >&2
                else
                    echo "$action_list_json"                         | python3 "$REPO_DIR/scripts/herdr-action-validate.py"                               --plugin=kimchi-remote                               --expected=open-picker                               --expected=refresh-mine                               --expected=open-worker-shell                               --expected=open-new-workspace                         || {
                            echo "install_herdr: WARNING one or more kimchi-remote actions referenced in .config/herdr/config.toml are missing from the installed plugin manifest." >&2
                            echo "install_herdr:          Check herdr plugin action list kimchi-remote to see what's available." >&2
                        }
                fi
            fi
        else
            echo "install_herdr: WARNING kimchi-remote install ran but the plugin is not discoverable via 'herdr plugin list' — bindings prefix+w / prefix+shift+w / prefix+alt+w / prefix+a will fail at runtime" >&2
        fi
    else
        echo "install_herdr: WARNING local/herdr-kimchi-remote/ not present in this checkout" >&2
        echo "install_herdr:          the four prefix+w/-shift+w/-alt+w/-a keybindings in .config/herdr/config.toml point at the kimchi-remote plugin and will be no-ops until the source directory exists." >&2
        echo "install_herdr:          to fix: clone the kimchi-remote plugin to local/herdr-kimchi-remote/ (it's not on github) and re-run install.sh -i herdr." >&2
        echo "install_herdr:          Either: git init && git remote add origin <your-internal-mirror> in local/herdr-kimchi-remote/" >&2
        echo "install_herdr:          or copy the plugin files from a peer who has them, then re-run install.sh." >&2
    fi
    run mkdir -p "$HOME/.config/herdr"
    link_or_backup "$REPO_DIR/.config/herdr/config.toml" "$HOME/.config/herdr/config.toml"
    # herdr-plus plugin config: projects / quick-actions / worktrees are
    # symlinked in, so the LOCAL generated dir (dotfiles-init.sh) wins; the
    # tracked dir is only the template fallback.
    run mkdir -p "$HOME/.config/herdr/plugins/config/cloudmanic.herdr-plus"
    for d in projects quick-actions worktrees; do
        local dst="$HOME/.config/herdr/plugins/config/cloudmanic.herdr-plus/$d"
        local src="$REPO_DIR/local/herdr/$d"
        [ -d "$src" ] || src="$REPO_DIR/.config/herdr/plugins/config/cloudmanic.herdr-plus/$d"
        link_or_backup "$src" "$dst"
    done
    # cdragon.command-center / glab-status / herdr-jira plugin configs are
    # fully repo-managed: each config dir is replaced with a symlink.
    for p in cdragon.command-center glab-status herdr-jira; do
        link_or_backup "$REPO_DIR/.config/herdr/plugins/config/$p" "$HOME/.config/herdr/plugins/config/$p"
    done
    # helper scripts (worktree-aware project opener, worktree-clean, pi shim)
    run mkdir -p "$HOME/.local/bin"
    link_or_backup "$REPO_DIR/scripts/service-repo-project" "$HOME/.local/bin/service-repo-project"
    link_or_backup "$REPO_DIR/scripts/worktree-clean" "$HOME/.local/bin/worktree-clean"
    link_or_backup "$REPO_DIR/scripts/pi" "$HOME/.local/bin/pi"
    # mr-review: GitLab MR AI review workflow (prefix+shift+v popup in
    # config.toml). Compiled from tools/mr-review — the ONLY remote-writing
    # component (mr-review-publish) is a separate binary.
    if command -v go >/dev/null 2>&1; then
        run bash -c "cd '$REPO_DIR/tools/mr-review' && go build -o '$HOME/.local/bin/mr-review' ./cmd/mr-review && go build -o '$HOME/.local/bin/mr-review-publish' ./cmd/mr-review-publish"
    else
        echo "install_herdr: go not found — skipping mr-review build (install Go, then run: cd $REPO_DIR/tools/mr-review && go build -o $HOME/.local/bin/mr-review ./cmd/mr-review && go build -o $HOME/.local/bin/mr-review-publish ./cmd/mr-review-publish)"
    fi
}

# ---- arg parsing ---------------------------------------------------------------
for arg in "$@"; do
  case "$arg" in
    -h|--help) usage; exit 0 ;;
    -d|--dry-run) DRY=1 ;;
    -f|--force) FORCE=1 ;;
    -c|--clean) CLEAN=1 ;;
    -i|--install) NEEDS_VALUE=1 ;;
    *)
      if [ "${NEEDS_VALUE:-0}" = 1 ]; then install_typ="$arg"; NEEDS_VALUE=0
      else echo "install: unknown argument: $arg" >&2; usage >&2; exit 2; fi ;;
  esac
done

[ "${NEEDS_VALUE:-0}" = 1 ] && { echo "install: -i requires a target" >&2; usage >&2; exit 2; }

if [ "${CLEAN:-0}" = 1 ]; then
  # a clean with no scope would leave configs unlinked AND installed nothing —
  # reject it outright (fail before touching anything)
  if [ -z "$install_typ" ]; then
    echo "install: -c/--clean requires a scope: -i <target> (herdr nvim zsh tmux vim) or -i all" >&2
    usage >&2
    exit 2
  fi
  if [ "$FORCE" != 1 ]; then
    echo "install: -c/--clean moves the SELECTED target's current configs to $BACKUP_ROOT before re-linking." >&2
    echo "install: this is destructive-ish — re-run with --force to confirm." >&2
    exit 1
  fi
  clean_install "$install_typ"
fi

if [ -z "$install_typ" ]; then
  usage
  exit 0
fi

case "$install_typ" in
    herdr)      install_herdr ;;
    fd)         install_fd ;;
    formatters) install_formatters ;;
    rainfrog)   install_rainfrog ;;
    vi-sql)     install_vi_sql ;;
    nvim)       install_nvim ;;
    zsh)        install_zsh ;;
    vim)        install_vim ;;
    tmux)       install_tmux ;;
    fzf)        install_fzf ;;
    repo)       install_gitconf ;;
    fonts)      install_fonts ;;
    bin)        install_bin ;;
    programing) install_programing ;;
    all)
        install_common
        install_herdr
        install_programing
        install_fd
        install_zsh
        install_tmux
        install_vim
        install_nvim
        install_formatters
        install_rainfrog
        install_vi_sql
        install_fonts
        install_gitconf
        install_fzf
        install_bin
    ;;
    *) echo "install: unknown target: $install_typ" >&2; usage >&2; exit 2 ;;
  esac

exit 0