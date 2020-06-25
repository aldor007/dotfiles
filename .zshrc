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
plugins=(aws git debian cp django node npm pip symfony2 tmux golang history history-substring-search git-prompt git-extras python nvm rust aws kubectl kube-ps1 zsh-autosuggestions)

source $ZSH/oh-my-zsh.sh

# User configuration

export PATH=$HOME/bin:/usr/local/bin:$PATH
# export MANPATH="/usr/local/man:$MANPATH"

# # Preferred editor for local and remote sessions
# if [[ -n $SSH_CONNECTION ]]; then
#   export EDITOR='vim'
# else
#   export EDITOR='mvim'
# fi

# Compilation flags
# export ARCHFLAGS="-arch x86_64"

# ssh
# export SSH_KEY_PATH="~/.ssh/dsa_id"

export TERM=xterm-256color
[ -n "$TMUX" ] && export TERM=screen-256color

export EDITOR='vim'
if [ -f ~/.bin/tmuxinator.zsh ]; then
    source ~/.bin/tmuxinator.zsh
fi

if [[ -f ~/.fzf.zsh ]]; then
    source ~/.fzf.zsh
fi

export PSZSH=$PS1

if [[ -f /etc/salt/grains ]]; then
    cat /etc/salt/grains | grep cde > /dev/null
    if [[ $? -eq 0 ]]; then
        trap SDKScript SIGUSR2
        SDKScript() {
            _OLD_PS1_CDE=$PSZSH
            source /tmp/cde_$$.sh
            rm /tmp/cde_$$.sh
            if [[ -z "$CDE_SDK_VERSION" ]]; then
                PS1="$PSZSH"
                export PS1
            else
                PS1="$CDE_SDK_VERSION $PSZSH"
                export PS1
            fi

            if [[ -f ~/.fzf.zsh ]]; then
                source ~/.fzf.zsh
            fi

        }
    fi
fi

export GPG_TTY=$(tty)    
export PATH="$HOME/.bin":$PATH

export NVM_DIR="$HOME/.nvm"
[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"  # This loads nvm

export PATH="$HOME/.tfenv/bin:$PATH"
