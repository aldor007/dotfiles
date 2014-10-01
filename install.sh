#!/bin/bash
base_dir=/tmp/dotfiles/

function install_common {
    sudo apt-get install git -y
    sudo apt-get install zip -y
    sudo apt-get install unzip -y
    sudo apt-get install subversion -y
    sudo apt-get install mercurial -y
    sudo apt-get install build-essentials -y
    sudo apt-get install python-pip -y
    sudo apt-get install python-fontforge -y
    sudo apt-get install screen -y 
    cp ${base_dir}.screenrc ~/.screenrc
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
    cp ${base_dir}.zshrc ~
    if [[ -f ~/.sdk_cde  ]]; then
        compaudit | sudo xargs chmod g-w
        compaudit | sudo xargs chown root
        rm ~/.zcompdump*
        compinit
    fi
    cp ${base_dir}zsh-themes/birav1.zsh-theme ~/.oh-my-zsh/themes

}

function install_tmux {
    sudo apt-get install tmux -y
    cp -r ${base_dir}.tmux ~/
    cp -r ${base_dir}.config/ ~/
    git clone https://github.com/Lokaltog/powerline   ~/.tmux/powerline2
    cd ~/.tmux/powerline2; sudo python setup.py install
    ln -s ~/.tmux/tmux.conf ~/.tmux.conf
}

function install_programing {
    sudo apt-get install nodejs
    sudo apt-get install python
    sudo apt-get install python-pip -y
}
function install_vim {
    sudo apt-get install fontforge --yes
    sudo apt-get install vim-nox --yes
    sudo apt-get install ctags --yes
    sudo pip install jedi -i http://pypi.python.org/simple/
    sudo pip install pylint -i http://pypi.python.org/simple/
    sudo npm install -g jslint
    sudo npm install -g jshint
    cp ${base_dir}.jshintrc ~/.jshintrc
    cp -r ${base_dir}.vim ~/
    git clone https://github.com/gmarik/Vundle.vim  ~/.vim/bundle/Vundle.vim
    cp ${base_dir}.pylintrc ~/
    cp ${base_dir}.vimrc ~/
    vim -c 'BundleInstall' -c qa
    cd ~/.vim/bundle/vimproc.vim/; make -f make_unix.mak
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
    cp ${base_dir}.gitconfig ~/

}
function clean_install {
    rm -rf ${base_dir}
    rm -rf ~/.vim
    rm -rf ~/.oh-my-zsh
    rm -rf ~/.zshrc
    rm -rf ~/.vimrc
    rm -rf ~/.tmux
    rm -rf ~/.tmux.conf

}




install_typ='all'
while [[ $# > 1 ]]
do
    key="$1"
    shift

    case $key in
        -c|--clean)
            clean_install
            shift
        ;;
        -i|--install)
            install_typ="$1"
            shift
        ;;
        *)
            echo "usage:
                    -c|--clean  - remove dotfiles before install
                    -i|--install [type] copy dotfiles
            "
            ;;
        esac
done
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
        install_repo
    ;;
    all)
        install_tmux
        install_zsh
        install_tmux
        install_fzf
        install_vim
        install_fonts
        install_programing
    ;;
    *)
        echo "Unknow"

    esac


