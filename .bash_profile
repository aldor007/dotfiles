# ~/.bash_profile — login shells.
# Historically an exact duplicate of .bashrc; source the single copy instead.
if [ -f "$HOME/.bashrc" ]; then
    . "$HOME/.bashrc"
fi