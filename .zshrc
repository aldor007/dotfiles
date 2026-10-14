# Path to your oh-my-zsh configuration.
ZSH=$HOME/.oh-my-zsh

# Set name of the theme to load.
# Look in ~/.oh-my-zsh/themes/
# Optionally, if you set this to "random", it'll load a random theme each
# time that oh-my-zsh is loaded.
ZSH_THEME="birav1"

# Example aliases
# alias zshconfig="mate ~/.zshrc"
# alias ohmyzsh="mate ~/.oh-my-zsh"

# Set to this to use case-sensitive completion
# CASE_SENSITIVE="true"

# Uncomment this to disable bi-weekly auto-update checks
# DISABLE_AUTO_UPDATE="true"

# Uncomment to change how often before auto-updates occur? (in days)
# export UPDATE_ZSH_DAYS=13

# Uncomment following line if you want to disable colors in ls
# DISABLE_LS_COLORS="true"

# Uncomment following line if you want to disable autosetting terminal title.
# DISABLE_AUTO_TITLE="true"

# Uncomment following line if you want to disable command autocorrection
# DISABLE_CORRECTION="true"

# Uncomment following line if you want red dots to be displayed while waiting for completion
# COMPLETION_WAITING_DOTS="true"

# Uncomment following line if you want to disable marking untracked files under
# VCS as dirty. This makes repository status check for large repositories much,
# much faster.
# DISABLE_UNTRACKED_FILES_DIRTY="true"

# Uncomment following line if you want to  shown in the command execution time stamp 
# in the history command output. The optional three formats: "mm/dd/yyyy"|"dd.mm.yyyy"|
# yyyy-mm-dd
# HIST_STAMPS="mm/dd/yyyy"

# Which plugins would you like to load? (plugins can be found in ~/.oh-my-zsh/plugins/*)
# Custom plugins may be added to ~/.oh-my-zsh/custom/plugins/
# Example format: plugins=(rails git textmate ruby lighthouse)
# nvm removed from plugins: it eagerly sources nvm (adds ~700ms); lazy-loaded below
plugins=(aws git debian cp node npm pip symfony2 tmux golang history-substring-search python rust kubectl kube-ps1 zsh-autosuggestions herdr)

# Skip oh-my-zsh update check (~90ms slower startup)
zstyle ':omz:update' mode disabled

source $ZSH/oh-my-zsh.sh

# User configuration

# Force English locale for the whole shell session. macOS defaults
# to pl_PL.UTF-8 from the system region, which makes lazygit and
# many other tools translate their UI to Polish. This override fixes
# lazygit + every other LANG-aware tool without per-tool config
# dances. Override later in this file (per-tool LANG settings) wins.
export LANG=en_US.UTF-8

export PATH=$HOME/bin:/usr/local/bin:$PATH
# export MANPATH="/usr/local/man:$MANPATH"

# # TERM is set by the terminal emulator; only override inside tmux
if [ -n "$TMUX" ]; then
    export TERM=screen-256color
fi

# Preferred editor for local and remote sessions
# if [[ -n $SSH_CONNECTION ]]; then
#   export EDITOR='vim'
# else
#   export EDITOR='mvim'
# fi

# Compilation flags
# export ARCHFLAGS="-arch x86_64"

# ssh
# export SSH_KEY_PATH="~/.ssh/dsa_id"

export EDITOR='vim'
if [ -f ~/.bin/tmuxinator.zsh ]; then
    source ~/.bin/tmuxinator.zsh
fi

if [[ -f ~/.fzf.zsh ]]; then
    source ~/.fzf.zsh
fi

export PSZSH=$PS1

export GPG_TTY=$(tty)    
export PATH="$HOME/.bin":$PATH

# Lazy nvm: node/npm/npx work immediately via the default version's PATH;
# full nvm only loads the first time you call `nvm` (saves ~1.2s startup)
export NVM_DIR="$HOME/.nvm"
export PATH="$NVM_DIR/versions/node/v24.16.0/bin:$PATH"
nvm() {
    unset -f nvm
    [ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"
    nvm "$@"
}


export PATH="$HOME/.tfenv/bin:$PATH"
export PATH=$PATH:$HOME/.istioctl/bin
export PATH=$PATH:/usr/local/go/bin
export PATH="$HOME/.tgenv/bin:$PATH"
export PATH="${KREW_ROOT:-$HOME/.krew}/bin:$PATH"

test -e "${HOME}/.iterm2_shell_integration.zsh" && source "${HOME}/.iterm2_shell_integration.zsh"

[ -s ~/.gvm/scripts/gvm ] && source ~/.gvm/scripts/gvm

# useful only for Mac OS Silicon M1, 
# # still working but useless for the other platforms
docker() {
    if [[ `uname -m` == "arm64" ]] && [[ "$1" == "run" || "$1" == "build" ]]; then
        /usr/local/bin/docker "$1" --platform linux/amd64 "${@:2}"
    else
        /usr/local/bin/docker "$@"
    fi
}

export PATH="$HOME/.poetry/bin:$PATH"
export PYENV_ROOT="$HOME/.pyenv"
command -v pyenv >/dev/null || export PATH="$PYENV_ROOT/bin:$PATH"
eval "$(pyenv init -)"
export PATH="$HOME/.local/bin:$PATH"

### MANAGED BY RANCHER DESKTOP START (DO NOT EDIT)
export PATH="/Users/mkaciuba/.rd/bin:$PATH"
### MANAGED BY RANCHER DESKTOP END (DO NOT EDIT)
#
export GOPROXY=https://us-central1-go.pkg.dev/cicd-master-oxk5/go-modules,https://proxy.golang.org,direct
export GONOSUMDB=github.com/castai/*,gitlab.com/castai/*
# export GOPRIVATE=gitlab.com/castai,github.com/castai
# opencode
export PATH=/Users/mkaciuba/.opencode/bin:$PATH

# Secrets live outside the repo (gitignored dotfiles shouldn't contain them)
[ -f ~/.secrets.zsh ] && source ~/.secrets.zsh

alias vim=nvim
# Override git-svn alias from oh-my-zsh git plugin
alias gsd='GSD_VERSION=2.78.1 node /Users/mkaciuba/.nvm/versions/node/v24.14.1/lib/node_modules/gsd-pi/dist/cli.js'
[ -f /opt/homebrew/share/google-cloud-sdk/path.zsh.inc ] && source /opt/homebrew/share/google-cloud-sdk/path.zsh.inc
# kimchi inside herdr panes: always start with experimental features enabled.
# herdr sets HERDR_ENV=1 in the panes it manages; outside herdr the flag is
# not added. `command kimchi` bypasses this function for the real binary.
kimchi() {
  if [ "${HERDR_ENV:-}" = "1" ]; then
    # Report as herdr's pi integration (source herdr:pi) so the pane gets a
    # resume-eligible session ref and is tracked again after a herdr server
    # restart. Same convention as the herdr-plus/herdr-jira TOML launches.
    env HERDR_AGENT=pi KIMCHI_AGENT_SOURCE=herdr:pi KIMCHI_AGENT_NAME=pi \
      kimchi --enable-experimental-features "$@"
  else
    command kimchi "$@"
  fi
}

alias pi=kimchi
