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
# Dotfiles repo location — overridable for worktrees/fresh machines; every
# script reference below uses this instead of a hardcoded ~/dotfiles.
export DOTFILES_DIR="${DOTFILES_DIR:-$HOME/dotfiles}"
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

# Default editor: nvim (vim binary itself is hijacked by alias below anyway).
# VISUAL is what git (core.editor unset), crontab -e, sudoedit, less 'v',
# and most tools consult first; EDITOR covers the rest.
export VISUAL='nvim'
export EDITOR='nvim'
if [ -f ~/.bin/tmuxinator.zsh ]; then
    source ~/.bin/tmuxinator.zsh
fi

if [[ -f ~/.fzf.zsh ]]; then
    source ~/.fzf.zsh
fi

export PSZSH=$PS1

export GPG_TTY=$(tty)    
export PATH="$HOME/.bin":$PATH

# Lazy nvm: node/npm/npx work immediately via the installed default's PATH;
# full nvm only loads the first time you call `nvm` (saves ~1.2s startup).
# The version is resolved dynamically (newest installed) — no hardcoded
# v24.x path, so node upgrades don't strand the PATH entry.
export NVM_DIR="$HOME/.nvm"
_nvm_newest=$(ls -d "$NVM_DIR"/versions/node/v* 2>/dev/null | sort -V | tail -1)
[ -n "$_nvm_newest" ] && [ -d "$_nvm_newest/bin" ] && export PATH="$_nvm_newest/bin:$PATH"
unset _nvm_newest
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
# Resolves the docker binary explicitly (Rancher Desktop ships it under
# ~/.rd/bin; /usr/local/bin is the Intel-era location) so the wrapper never
# recurses into itself and works regardless of which install is present.
# docker: passthrough, native architecture. `command -v docker` would return
# THIS function, so discovery scans PATH dirs ($path) first, then falls back
# to common macOS locations (Homebrew, Rancher Desktop). `command docker …`
# inside the wrappers bypasses the function, avoiding recursion.
_resolve_docker() {
    local d
    for d in $path /opt/homebrew/bin /usr/local/bin "$HOME/.rd/bin"; do
        [ -e "$d/docker" ] && [ -x "$d/docker" ] && { printf '%s\n' "$d/docker"; return 0; }
    done
    echo "docker: no binary found (PATH, /opt/homebrew/bin, /usr/local/bin, \$HOME/.rd/bin)" >&2
    return 127
}
docker() {
    local d; d=$(_resolve_docker) || return $?
    "$d" "$@"
}
docker-amd64() {
    local d; d=$(_resolve_docker) || return $?
    if [[ "$1" == "run" || "$1" == "build" ]]; then
        "$d" "$1" --platform linux/amd64 "${@:2}"
    else
        "$d" "$@"
    fi
}

export PATH="$HOME/.poetry/bin:$PATH"
export PYENV_ROOT="$HOME/.pyenv"
if command -v pyenv >/dev/null 2>&1 || [ -x "$PYENV_ROOT/bin/pyenv" ]; then
  command -v pyenv >/dev/null 2>&1 || export PATH="$PYENV_ROOT/bin:$PATH"
  eval "$(pyenv init -)"
else
  unset PYENV_ROOT
fi
export PATH="$HOME/.local/bin:$PATH"

### MANAGED BY RANCHER DESKTOP START (DO NOT EDIT)
export PATH="/Users/mkaciuba/.rd/bin:$PATH"
### MANAGED BY RANCHER DESKTOP END (DO NOT EDIT)
# opencode
export PATH="$HOME/.opencode/bin:$PATH"

# Secrets live outside the repo (gitignored dotfiles shouldn't contain them)
[ -f ~/.secrets.zsh ] && source ~/.secrets.zsh

# -- Go module proxy -----------------------------------------------------------
# The internal mirror URL embeds a GCP project ID, so it is NOT tracked here:
# set WORK_GOPROXY in ~/.secrets.zsh (gitignored); public proxy is the
# portable fallback. Commas are normalized so a missing/extra trailing comma
# in WORK_GOPROXY cannot produce an invalid GOPROXY.
# export GOPRIVATE=gitlab.com/<your-org>,github.com/<your-org>  (optional, uncomment)
if [ -n "${WORK_GOPROXY:-}" ]; then
  case "$WORK_GOPROXY" in
    *,) export GOPROXY="${WORK_GOPROXY}https://proxy.golang.org,direct" ;;
    *)  export GOPROXY="${WORK_GOPROXY},https://proxy.golang.org,direct" ;;
  esac
else
  export GOPROXY="https://proxy.golang.org,direct"
fi
export GONOSUMDB=github.com/<your-org>/*,gitlab.com/<your-org>/*

# herdr-projects board reads pi transcripts from $PI_CODING_AGENT_DIR/sessions
# (scripts/pi adapter writes kimchi sessions there)
export PI_CODING_AGENT_DIR="$HOME/.pi/agent"

alias vim=nvim
# Override git-svn alias from oh-my-zsh git plugin
# Resolve the gsd-pi CLI through npm's global root so a node/nvm upgrade
# doesn't break the hardcoded version path.
alias gsd='GSD_VERSION=2.78.1 node "$(npm root -g)/gsd-pi/dist/cli.js"'
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

# --- Kimchi LLM gateway key ----------------------------------------------------
# kimchi's own gateway (llm.kimchi.dev/openai/v1) is OpenAI-compatible; minuet
# (inline completion) reads this env var. Source it from kimchi's config.json.
if [[ -z "$KIMCHI_API_KEY" && -r "$HOME/.config/kimchi/config.json" ]]; then
  export KIMCHI_API_KEY=$(jq -r '.apiKey // empty' ~/.config/kimchi/config.json 2>/dev/null)
fi

# --- morning monitor: PR/MR digest --------------------------------------------
# macOS ships no `timeout` (coreutils' gtimeout); fall back so the refresh
# below never fails silently.
if ! command -v timeout >/dev/null 2>&1; then
  if command -v gtimeout >/dev/null 2>&1; then
    timeout() { gtimeout "$@"; }
  else
    timeout() { "$@"; }
  fi
fi
# Once/day per machine: prints cached summary, kicks a background refresh.
# Skipped inside herdr panes (HERDR_ENV=1) so restored/hosted panes stay quiet.
# Kill-switch: PR_SUMMARY_AUTO=0. Fresh run on demand: `prs`.
alias prs="$DOTFILES_DIR/scripts/pr-summary.sh"
alias prsa="$DOTFILES_DIR/scripts/pr-summary.sh --analyze"
_pr_summary_morning() {
  [ -n "${HERDR_ENV:-}" ] && return 0
  [ "${PR_SUMMARY_AUTO:-1}" = "0" ] && return 0
  local cache="$HOME/.cache/pr-summary"
  local script="$DOTFILES_DIR/scripts/pr-summary.sh"
  [ -x "$script" ] || return 0
  mkdir -p "$cache"
  local stamp="$cache/stamp-$(date +%F)"

  if [ -f "$cache/output.txt" ]; then
    printf '\n%s PR/MR digest (cached, refreshes once/day in background; `prs` = live) %s\n' "---" "---"
    cat "$cache/output.txt"
    if [ ! -e "$stamp" ]; then
      rm -f "$cache"/stamp-* 2>/dev/null; touch "$stamp"
      ( timeout 420 "$script" >| "$cache/output.txt" 2>&1 &! )
    fi
  else
    # First day ever: run once in the foreground so you see today's state now.
    touch "$stamp"
    timeout 420 "$script" | tee "$cache/output.txt"
  fi
}
_pr_summary_morning
export PATH="/opt/homebrew/opt/libpq/bin:$PATH"
