#!/bin/bash
base_dir=~/dotfiles/

function install_common {
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
  wget https://github.com/Lokaltog/powerline/raw/develop/font/PowerlineSymbols.otf
  wget https://github.com/Lokaltog/powerline/raw/develop/font/10-powerline-symbols.conf
  mkdir -p ~/.fonts
  mv PowerlineSymbols.otf ~/.fonts/
  mkdir -p ~/.config/fontconfig/conf.d
  mv 10-powerline-symbols.conf ~/.config/fontconfig/conf.d/
  fc-cache -vf ~/.fonts/
}

function install_zsh {
    sudo apt-get install zsh -y
    sudo chsh -s /bin/zsh  $USER
    # cp -r ${base_dir}.oh-my-zsh/ ~g
    git clone https://github.com/robbyrussell/oh-my-zsh ~/.oh-my-zsh
    ln -s  ${base_dir}.zshrc ~/.zshrc
    git clone https://github.com/zsh-users/zsh-autosuggestions ~/.oh-my-zsh/plugins/zsh-autosuggestions
    ln -s ${base_dir}zsh-themes/birav1.zsh-theme ~/.oh-my-zsh/themes
}

function install_tmux {
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
    sudo apt-get install python -y
    sudo apt-get install python-pip -y
    curl -o- https://raw.githubusercontent.com/creationix/nvm/v0.31.4/install.sh | bash

    export NVM_DIR="$HOME/.nvm"
    [ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"  # This loads nvm
    nvm install stable
    nvm use stable
    npm install -g jslint
    npm install -g eslint
    npm install eslint-plugin-jasmine -g
}

function install_vim {
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
    ln -s  ${base_dir}.gitconfig ~/.gitconfig
    git clone https://github.com/jonas/tig /tmp/tig
    cd /tmp/tig; sudo make prefix=/usr/local
    cd /tmp/tig; sudo make install prefix=/usr/local
}

function install_bin {
    mkdir -p ~/.bin/
    cp -r ${base_dir}/scripts/* ~/.bin
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
git clone --recursive https://github.com/Aldor007/dotfiles ${base_dir}
case $install_typ in
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
        install_tmux
        install_programing
        install_zsh
        install_tmux
        install_vim
        install_fonts
        install_gitconf
        install_fzf
        install_bin
    ;;
    *)
        echo "Unknow"
    esac
