#!/bin/bash
base_dir=~/dotfiles/

function install_common {
    command -v apt-get >/dev/null 2>&1 || { echo "install_common: apt-get not found — skipping (Debian/Ubuntu only)"; return; }
    sudo apt-get install git -y 
    sudo apt-get install zip -y 
    sudo apt-get install unzip -y 
    sudo apt-get install subversion -y 
    sudo apt-get install mercurial -y 
    sudo apt-get install build-essential -y 
    sudo apt-get install python-pip -y 

    sudo apt-get install screen -y  
    ln -s  ${base_dir}.screenrc ~/.screenrc
}

function install_fonts {
  command -v apt-get >/dev/null 2>&1 || { echo "install_fonts: skipping (Debian/Ubuntu only)"; return; }
  wget https://github.com/Lokaltog/powerline/raw/develop/font/PowerlineSymbols.otf
  wget https://github.com/Lokaltog/powerline/raw/develop/font/10-powerline-symbols.conf
  mkdir -p ~/.fonts
  mv PowerlineSymbols.otf ~/.fonts/
  mkdir -p ~/.config/fontconfig/conf.d
  mv 10-powerline-symbols.conf ~/.config/fontconfig/conf.d/
  fc-cache -vf ~/.fonts/
}

function install_zsh {
    command -v apt-get >/dev/null 2>&1 || { echo "install_zsh: skipping (Debian/Ubuntu only)"; return; }
    sudo apt-get install zsh -y
    sudo chsh -s /bin/zsh  $USER
    # cp -r ${base_dir}.oh-my-zsh/ ~g
    git clone https://github.com/robbyrussell/oh-my-zsh ~/.oh-my-zsh
    ln -s  ${base_dir}.zshrc ~/.zshrc
    git clone https://github.com/zsh-users/zsh-autosuggestions ~/.oh-my-zsh/plugins/zsh-autosuggestions
    ln -s ${base_dir}zsh-themes/birav1.zsh-theme ~/.oh-my-zsh/themes
}

function install_tmux {
    command -v apt-get >/dev/null 2>&1 || { echo "install_tmux: skipping (Debian/Ubuntu only)"; return; }
    sudo apt-get install python-fontforge -y 
    ubuntu=$(cat /etc/issue | grep -i ubuntu)
    if [ $? -eq 0 ]; then
        sudo apt-get install -y python-software-properties software-properties-common
        sudo add-apt-repository -y ppa:pi-rho/dev
        sudo apt-get update
    fi
    sudo apt-get install tmux -y
    sudo apt-get install python-netifaces -y
    ln -s ${base_dir}.tmux ~/.tmux
    ln -s ${base_dir}.config/powerline/ ~/.config/powerline
    git clone https://github.com/Lokaltog/powerline   ~/.tmux/powerline2
    cd ~/.tmux/powerline2; sudo python setup.py install
    ln -s ${base_dir}.tmux/tmux.conf ~/.tmux.conf
}

function install_programing {
    sudo apt-get install python -y 2>/dev/null || true
    sudo apt-get install python-pip -y 2>/dev/null || true
    curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.1/install.sh | bash

    export NVM_DIR="$HOME/.nvm"
    [ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"  # This loads nvm
    nvm install stable
    nvm use stable
    npm install -g jslint
    npm install -g eslint
    mkdir -p ~/.bin
    if [ "$(uname -s)" = "Linux" ]; then
        curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && mv kubectl ~/.bin/kubectl && chmod a+x ~/.bin/kubectl
    else
        echo "kubectl: skipping (linux/amd64 binary only; use brew install kubectl instead)"
    fi
    curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash 
    curl -sL https://istio.io/downloadIstioctl | sh -
    git clone https://github.com/cunymatthieu/tgenv.git ~/.tgenv
    if [ "$(uname -s)" = "Linux" ]; then
        curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash 
        cd /tmp && curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip"  && unzip awscliv2.zip &&     sudo ./aws/install 
    else
        echo "awscli/azure: skipping (Linux installers; use brew install awscli azure-cli)"
    fi
    git clone https://github.com/tfutils/tfenv.git ~/.tfenv
    # krew (repeatable, idempotent-ish block — one copy; was duplicated here)
    (
  set -x; cd "$(mktemp -d)" &&
  OS="$(uname | tr '[:upper:]' '[:lower:]')" &&
  ARCH="$(uname -m | sed -e 's/x86_64/amd64/' -e 's/\(arm\)\(64\)\?.*/\1\2/' -e 's/aarch64$/arm64/')" &&
  KREW="krew-${OS}_${ARCH}" &&
  curl -fsSLO "https://github.com/kubernetes-sigs/krew/releases/latest/download/${KREW}.tar.gz" &&
  tar zxvf "${KREW}.tar.gz" &&
  ./"${KREW}" install krew
)
}

function install_vim {
    command -v apt-get >/dev/null 2>&1 || { echo "install_vim: skipping (Debian/Ubuntu only)"; return; }
    sudo apt-get install fontforge --yes
    sudo apt-get install vim-nox --yes
    sudo apt-get install ctags --yes
    sudo pip install jedi -i http://pypi.python.org/simple/
    sudo pip install pylint -i http://pypi.python.org/simple/
    rm -f ~/.vimrc
    ln -s ${base_dir}.jshintrc ~/.jshintrc
    ln -s ${base_dir}.eslintrc.json  ~/.eslintrc.json
    ln -s  ${base_dir}.vim ~/.vim
    git clone https://github.com/gmarik/Vundle.vim  ~/.vim/bundle/Vundle.vim
    ln -s ${base_dir}.pylintrc ~/.pylintrc
    ln -s ${base_dir}.vimrc ~/.vimrc
    echo | vim -c 'BundleInstall' -c qa
}
function install_fzf {
    git clone https://github.com/junegunn/fzf.git ~/.fzf
    ~/.fzf/install --all
}

function install_gitconf {
    command -v apt-get >/dev/null 2>&1 || { echo "install_gitconf: skipping (Debian/Ubuntu only)"; return; }
    sudo apt-get install libncurses5-dev libncursesw5-dev -y
    ln -s  ${base_dir}.gitconfig ~/.gitconfig
    git clone https://github.com/jonas/tig /tmp/tig
    cd /tmp/tig; sudo make prefix=/usr/local
    cd /tmp/tig; sudo make install prefix=/usr/local
}

function install_bin {
    mkdir -p ~/.bin/
    cp -r ${base_dir}/scripts/* ~/.bin
}

# Formatters/linters used by conform.nvim + nvim-lint (nvim/lua/plugins/format.lua).
# macOS: everything via brew. Debian/Ubuntu: apt only ships shellcheck; the
# rest come from their own toolchains (cargo install stylua, pipx install
# ruff, npm -g prettier). Missing binaries are skipped gracefully by conform.
function install_formatters {
    if command -v brew >/dev/null 2>&1; then
        brew install stylua ruff prettier shellcheck
        echo "formatters installed: stylua ruff prettier shellcheck"
    elif command -v apt-get >/dev/null 2>&1; then
        sudo apt-get install -y shellcheck
        echo "apt: shellcheck installed; install stylua (cargo), ruff (pipx), prettier (npm) separately"
    else
        echo "install_formatters: no brew/apt found — install stylua, ruff, prettier, shellcheck manually"
    fi
}

# vi-sql: vim-mode SQL TUI (schema browser, grid editing, EXPLAIN, MCP
# server for AI). brew on macOS; pinned curl installer elsewhere.
function install_vi_sql {
    if command -v brew >/dev/null 2>&1; then
        brew install vi-sql
    else
        VI_SQL_VERSION="${VI_SQL_VERSION:-v0.6.0}" sh -c "$(curl -fsSL https://vi-sql.com/install.sh)"
    fi
    echo "vi-sql installed: vi-sql --connect <dsn> (or sql-db.sh --visql)"
}

# rainfrog: spreadsheet-style DB TUI (Postgres/MySQL/SQLite).
# macOS via brew; elsewhere via cargo. Config is repo-managed (no secrets;
# connections come from sql-db.sh --rainfrog with a fresh IAM DSN).
function install_rainfrog {
    if command -v brew >/dev/null 2>&1; then
        brew install rainfrog
    elif command -v cargo >/dev/null 2>&1; then
        cargo install rainfrog --locked
    else
        echo "install_rainfrog: no brew/cargo found — install manually (brew install rainfrog)"
        return
    fi
    case "$(uname -s)" in
        Darwin) conf="$HOME/Library/Application Support/dev.rainfrog.rainfrog" ;;
        *)      conf="$HOME/.config/rainfrog" ;;
    esac
    mkdir -p "$conf"
    # local generated config (dotfiles-init.sh) wins; tracked base is fallback
    src=${base_dir}local/rainfrog/rainfrog_config.toml
    [ -f "$src" ] || src=${base_dir}.config/rainfrog/rainfrog_config.toml
    ln -sf "$src" "$conf/rainfrog_config.toml"
    echo "rainfrog installed; config linked -> $conf/rainfrog_config.toml"
}

function clean_install {
    echo "Clean install"
    rm -rf ~/.vim
    rm -rf ~/.oh-my-zsh
    rm -rf ~/.zshrc
    rm -rf ~/.vimrc
    rm -rf ~/.tmux
    rm -rf ~/.tmux.conf
    rm -rf ~/.screenrc
}

install_typ='all'
key="$1"

case $key in
    -c|--clean)
        clean_install
        shift
    ;;
    -i|--install)
        install_typ="$2"
        shift
    ;;
    *)
        echo "usage:
                -c|--clean  - remove dotfiles before install
                -i|--install [type] copy dotfiles
        "
        ;;
    esac
install_common
[ -d "${base_dir}" ] || git clone --recursive https://github.com/Aldor007/dotfiles ${base_dir}
function install_herdr {
    curl -fsSL https://herdr.dev/install.sh | sh
    # All herdr plugins are installed from this list; their configs live in
    # this repo and get symlinked in below, so the repo stays the single
    # source of truth for every plugin install + config.
    herdr plugin install cloudmanic/herdr-plus
    herdr plugin install --yes speardragon/herdr-command-center
    herdr plugin install --yes ChmaraX/herdr-nvim
    herdr plugin install --yes Davidcreador/herdr-token-dashboard
    herdr plugin install --yes jpwallace22/herdr-glab-status
    herdr plugin install --yes a2u/herdr-jira
    herdr plugin install --yes robbyrussell/herdr-ohmyzsh
    herdr plugin install --yes persiyanov/herdr-reviewr
    # herdr-bar: Cmd+K fuzzy jump (takes over auto tab titles now that
    # herdr-automatic-rename is removed; prefix+k binding in config.toml)
    herdr plugin install --yes jeffarese/herdr-bar
    mkdir -p ~/.config/herdr
    ln -sf ${base_dir}.config/herdr/config.toml ~/.config/herdr/config.toml
    # herdr-plus plugin config: projects / quick-actions / worktrees are
    # symlinked in, so the LOCAL generated dir (dotfiles-init.sh) wins; the
    # tracked dir is only the template fallback.
    mkdir -p ~/.config/herdr/plugins/config/cloudmanic.herdr-plus
    for d in projects quick-actions worktrees; do
        dst=~/.config/herdr/plugins/config/cloudmanic.herdr-plus/$d
        src=${base_dir}local/herdr/$d
        [ -d "$src" ] || src=${base_dir}.config/herdr/plugins/config/cloudmanic.herdr-plus/$d
        rm -rf "$dst"
        ln -s "$src" "$dst"
    done
    # cdragon.command-center / glab-status / herdr-jira plugin configs are
    # fully repo-managed: each config dir is replaced with a symlink into this repo.
    for p in cdragon.command-center glab-status herdr-jira; do
        dst=~/.config/herdr/plugins/config/$p
        rm -rf "$dst"
        ln -s ${base_dir}.config/herdr/plugins/config/$p "$dst"
    done
    # service-repo-project helper script (worktree-aware project opener)
    mkdir -p ~/.local/bin
    ln -sf ${base_dir}scripts/service-repo-project ~/.local/bin/service-repo-project
    # worktree-clean helper script (inactive worktree cleanup)
    ln -sf ${base_dir}scripts/worktree-clean ~/.local/bin/worktree-clean
    # pi -> kimchi shim so herdr's pi integration can resume sessions
    # (herdr runs canonical `pi --session <path>`; there is no pi binary)
    ln -sf ${base_dir}scripts/pi ~/.local/bin/pi
}

case $install_typ in
    herdr)
        install_herdr
    ;;
    formatters)
        install_formatters
    ;;
    rainfrog)
        install_rainfrog
    ;;
    vi-sql)
        install_vi_sql
    ;;
    zsh)
        install_zsh
    ;;
    vim)
        install_vim
    ;;
    tmux)
        install_tmux
    ;;
    fzf)
        install_fzf
    ;;
    repo)
        install_gitconf
    ;;
    all)
        install_herdr
        install_programing
        install_zsh
        install_tmux
        install_vim
        install_formatters
        install_rainfrog
        install_vi_sql
        install_fonts
        install_gitconf
        install_fzf
        install_bin
    ;;
    *)
        echo "Unknow"
    esac
