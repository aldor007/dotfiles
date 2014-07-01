#!/bin/bash
function install_common {
    sudo apt-get install git -y
    sudo apt-get install zip -y
    sudo apt-get install unzip -y
    sudo apt-get install subversion -y
    sudo apt-get install mercurial -y
}
function install_zsh {

    sudo apt-get install zsh -y
    sudo chsh -s /bin/zsh  $USER
    cp -r .oh-my-zsh/ ~
    cp .zshrc ~

}

function install_tmux {
    sudo apt-get install tmux -y
    cp -r .tmux ~/
    cp .tmux.conf ~/
}

function install_programing {
    sudo apt-get install nodejs -y
    sudo apt-get install python -y
    sudo apt-get install python-pip -y
}
function install_vim {
    sudo apt-get install fontforge --yes
    sudo apt-get install ctags --yes
    sudo pip install jedi -i http://pypi.python.org/simple/
    sudo pip install pylint -i http://pypi.python.org/simple/
    sudo npm install -g jslint
    cp -r .vim ~/
    cp .pylintrc ~/
    cp .vimrc ~/
}
function install_fzf {
    sudo apt-get install ruby -y
    sudo apt-get install ruby-dev -y
    sudo apt-get install rubygem -y
    sudo apt-get install ruby-ncurses -y
    sudo apt-get install libncurses5-dev -y
    git clone https://github.com/junegunn/fzf.git ~/.fzf
    ~/.fzf/install
} 

function install_repo_file {
    cp .gitconfig ~/

}



install_common
install_zsh
install_tmux
install_programing
install_vim
install_fzf
