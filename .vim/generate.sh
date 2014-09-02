#!/bin/bash
mkdir ~/.vim/tags
cd /var/cache/apt
dpkg -c libc6-dev_2.19-7_amd64.deb | grep -o '/usr/include/.*\.h'> ~/.vim/tags/libc6-filelist
dpkg -c libstdc++6_4.9.0-7_i386.deb | grep -E -o '/usr/include/.*\.(h|hpp)' >> ~/.vim/tags/stdlibcpp-filelist
dpkg -c libboost1.54-dev_1.54.0-5+b1_amd64.deb |  grep -E -o '/usr/include/.*\.(h|hpp)' | grep -v '/usr/include/boost/typeof/' > ~/.vim/tags/boost-filelist
ctags --sort=foldcase --c++-kinds=+p --fields=+iaS --extra=+q  -f ~/.vim/tags/libc6 -L ~/.vim/tags/libc6-filelist
ctags --sort=foldcase -R --c++-kinds=+p --fields=+iaS --extra=+q -f ~/.vim/tags/stdlibcpp -L ~/.vim/tags/stdlibcpp-filelist
ctags --sort=foldcase --c++-kinds=+p --fields=+iaS --extra=+q -f ~/.vim/tags/boost -L ~/.vim/tags/boost-filelist
