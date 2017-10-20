scp -r $USER@$1:~/.ssh ~/
cat ~/.ssh/id_rsa.pub >> ~/.ssh/authorized_keys
chmod g-w $HOME
