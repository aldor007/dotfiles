    set nocompatible        " must be first line
    set background=dark     " Assume a dark background
    "Vundle  autoload
    filetype off
    set runtimepath+=~/.vim/bundle/Vundle.vim
    set runtimepath^=~/.vim/bundle/ctrlp.vim

    call vundle#rc()
    Bundle 'gmarik/Vundle.vim'

    " set autochdir
    " auto read file change
    set autoread

    " Plugins
    " automatic closing of quotes
    Bundle 'Raimondi/delimitMate'
    " git wraper
    Bundle 'tpope/vim-fugitive'
    " better history
    Bundle 'sjl/gundo.vim'
    " tabulairz
    Bundle 'godlygeek/tabular'
    Bundle 'kien/ctrlp.vim'
    Bundle 'tpope/vim-commentary'
    Bundle 'vim-scripts/tlib'
    Bundle 'SirVer/ultisnips'
    Bundle 'honza/vim-snippets'
    Bundle 'mhinz/vim-startify'
    Bundle 'Lokaltog/vim-distinguished'
    Bundle 'bling/vim-airline'
    Bundle 'scrooloose/syntastic'
    Bundle 'scrooloose/nerdtree'
    Bundle 'rodjek/vim-puppet'
    Bundle 'evanmiller/nginx-vim-syntax'
    Bundle 'airblade/vim-gitgutter'
    Bundle 'majutsushi/tagbar'
    Bundle 'mileszs/ack.vim'
    Bundle 'xaizek/vim-inccomplete'
    Bundle 'Shougo/neocomplete.vim'
    Bundle 'wellle/tmux-complete.vim'
    Bundle 'Shougo/unite.vim'
    Bundle 'Shougo/vimproc.vim'
    Bundle 'Shougo/vimfiler.vim'
    Bundle 'evidens/vim-twig'
    Bundle 'plasticboy/vim-markdown'
    Bundle 'DavidEGx/ctrlp-smarttabs'
    Bundle 'terryma/vim-multiple-cursors'
    Bundle 'pbrisbin/vim-mkdir'
    Bundle 'tpope/vim-surround'
    Bundle "tmhedberg/matchit"
    Bundle "sickill/vim-pasta"

        " Dla putty
        " let g:NERDTreeDirArrows=0
        set encoding=utf-8
" General
        set background=dark         " Assume a dark background
        filetype plugin indent on       " Automatically detect file types.
        syntax on                                       " syntax highlighting
        syntax enable

        set mouse=a                                     " automatically enable mouse usage
       " set autochdir                           " always switch to the current file directory.. Messes with some plugins, best left commented out
        " not every vim is compiled with this, use the following line instead
        " If you use command-t plugin, it conflicts with this, comment it out.
     "autocmd BufEnter * if bufname("") !~ "^\[A-Za-z0-9\]*://" | lcd %:p:h | endif
        scriptencoding utf-8

        " set autowrite                  " automatically write a file when leaving a modified buffer
        set shortmess+=filmnrxoOtT      " abbrev. of messages (avoids 'hit enter')
        set viewoptions=folds,options,cursor,unix,slash " better unix / windows compatibility
        set virtualedit=onemore                 " allow for cursor beyond last character
        set history=1000                                " Store a ton of history (default is 20)
        " set nospell                                     " spell checking off
        set spell spelllang=en_us
        set backup                                              " backups are nice
        set noswapfile
        set undofile                                    " so is persistent undo ...
        set undolevels=1000 "maximum number of changes that can be undone
        set undoreload=10000 "maximum number lines to save for undo on a buffer reload
        au BufWinLeave * silent! mkview  "make vim save view (state) (folds, cursor, etc)
        au BufWinEnter * silent! loadview "make vim load view (state) (folds, cursor, etc)
        " }
" }
 " Syntastic
     let g:syntastic_enable_signs=1
    "  let g:syntastic_auto_jump=1
     let g:syntastic_stl_format = '[%E{Err: %fe #%e}%B{, }%W{Warn: #%w}]'

     set statusline+=%#warningmsg#
     set statusline+=%{SyntasticStatuslineFlag()}
     set statusline+=%*
     let g:syntastic_c_no_include_search = 1
     let g:syntastic_cpp_compiler_options = ' -std=c++11'
     " Needed for airline to work
     set laststatus=2
" Vim UI {
        colorscheme distinguished " deser load a colorscheme
        set tabpagemax=15                               " only show 15 tabs
        set showmode                    " display the current mode
        set cursorline                                  " highlight current line
        hi cursorline guibg=#333333     " highlight bg color of current line
        hi CursorColumn guibg=#333333   " highlight cursor

        set include=^\\s*#\\s*include\ \\(<boost/\\)\\@!

        set backspace=indent,eol,start  " backspace for dummys
        set linespace=0                                 " No extra spaces between rows
        set nu                                                  " Line numbers on
        set showmatch                                   " show matching brackets/parenthesis
        set incsearch                                   " find as you type search
        set hlsearch                                    " highlight search terms
        set winminheight=0                              " windows can be 0 line high
        set ignorecase                                  " case insensitive search
        set smartcase                                   " case sensitive when uc present
        set wildmenu                                    " show list instead of just completing
        set wildmode=list:longest,full  " command <Tab> completion, list matches, then longest common part, then all.
        set whichwrap=b,s,h,l,<,>,[,]   " backspace and cursor keys wrap to
        set scrolljump=5                                " lines to scroll when cursor leaves screen
        set scrolloff=3                                 " minimum lines to keep above and below cursor
        set foldenable                                  " auto fold code
        set gdefault                                    " the /g flag on :s substitutions by default
        set list
        set listchars=tab:>.,trail:.,extends:#,nbsp:. " Highlight problematic whitespace
        set relativenumber

" } 
"   Formatting {
        set wrap                      " wrap long lines
        set autoindent                  " indent at the same level of the previous line
        set shiftwidth=4                " use indents of 4 spaces
        set expandtab                           " tabs are spaces, not tabs
        set tabstop=4                                   " an indentation every four columns
        set softtabstop=4                               " let backspace delete indent
        set smarttab
        "set matchpairs+=<:>                    " match, to be used with %
        set pastetoggle=<F12>           " pastetoggle (sane indentation on pastes)
        set comments=sl:/*,mb:*,elx:*/  " auto format comment blocks
        au BufRead,BufNewFile /etc/nginx/*,/usr/local/nginx/conf/* if &ft == '' | setfiletype nginx | endif
        au BufRead,BufNewFile *.md,*.markdown setfiletype markdown
        au BufRead,BufNewFile *.cu,*.ch  setfiletype cpp

        " Remove trailing whitespaces and ^M chars - not work
        " autocmd FileType c,cpp,java,php,js,python,twig,xml,yml autocmd BufWritePre <buffer> :call setline(1,map(getline(1,"$"),'substitute(v:val,"\\s\\+$","","")'))
        " autocmd FileType python compiler pylint
" }

"  Show && Remove trailing whitespaces in commons source files on save
         autocmd FileType c,cpp,java,php,javascript,python,yaml,json,sls,puppet  autocmd BufWritePre <buffer> :%s/\s\+$//e

         highlight ExtraWhitespace ctermbg=red guibg=red
         match ExtraWhitespace /\s\+$/
         autocmd BufWinEnter * match ExtraWhitespace /\s\+$/
         autocmd InsertEnter * match ExtraWhitespace /\s\+\%#\@<!$/
         autocmd InsertLeave * match ExtraWhitespace /\s\+$/
         autocmd BufWinLeave * call clearmatches()
" Key (re)Mappings {

        "The default leader is '\', but many people prefer ',' as it's in a standard
        "location
        let mapleader = ','

    " Making it so ; works like : for commands. Saves typing and eliminates :W style typos due to lazy holding shift.
        nnoremap ; :



        " Easier moving in tabs and windows
        map <C-J> <C-W>j<C-W>_
        map <C-K> <C-W>k<C-W>_
        map <C-L> <C-W>l<C-W>_
        map <C-H> <C-W>h<C-W>_

    " Wrapped lines goes down/up to next row, rather than next line in file.
    nnoremap j gj
    nnoremap k gk

        " The following two lines conflict with moving to top and bottom of the
        " screen
        " If you prefer that functionality, comment them out.
        map <S-H> gT
        map <S-L> gt
        " Stupid shift key fixes
        cmap WQ wq
        cmap wQ wq
        cmap Tabe tabe

        " Yank from the cursor to the end of the line, to be consistent with C and D.
        nnoremap Y y$


        """ Code folding options
        nmap <leader>f0 :set foldlevel=0<CR>
        nmap <leader>f1 :set foldlevel=1<CR>
        nmap <leader>f2 :set foldlevel=2<CR>
        nmap <leader>f3 :set foldlevel=3<CR>
        nmap <leader>f4 :set foldlevel=4<CR>
        nmap <leader>f5 :set foldlevel=5<CR>
        nmap <leader>f6 :set foldlevel=6<CR>
        nmap <leader>f7 :set foldlevel=7<CR>
        nmap <leader>f8 :set foldlevel=8<CR>
        nmap <leader>f9 :set foldlevel=9<CR>

    "clearing highlighted search
        nmap <silent> <leader>/ :nohlsearch<CR>

        " Shortcuts
        " Change Working Directory to that of the current file
        cmap cwd lcd %:p:h
        cmap cd. lcd %:p:h

        " visual shifting (does not exit Visual mode)
        vnoremap < <gv
        vnoremap > >gv
        " For when you forget to sudo.. Really Write the file.
        cmap w!! w !sudo tee % >/dev/null
        augroup line_return
                au!
                    au BufReadPost *
                            \ if line("'\"") > 0 && line("'\"") <= line("$") |
                            \     execute 'normal! g`"zvzz' |
                            \ endif
        augroup END
" }

" Plugins {

            let g:tmuxcomplete#trigger = ''
        " NeoComplate
            " Note: This option must set it in .vimrc(_vimrc).  NOT IN
            " .gvimrc(_gvimrc)!
            " Disable AutoComplPop.
            let g:acp_enableAtStartup = 1
    
            let g:neocomplete#force_overwrite_completefunc = 1
            " Use neocomplete.
            let g:neocomplete#enable_at_startup = 1
            " Use smartcase.
            let g:neocomplete#enable_smart_case = 1
            " Set minimum syntax keyword length.
            let g:neocomplete#sources#syntax#min_keyword_length = 3
            let g:neocomplete#lock_buffer_name_pattern = '\*ku\*'

            " Define dictionary.
            let g:neocomplete#sources#dictionary#dictionaries = {
                \ 'default' : '',
                \ 'vimshell' : $HOME.'/.vimshell_hist',
                \ 'scheme' : $HOME.'/.gosh_completions'
                    \ }
            " Define keyword.
            if !exists('g:neocomplete#keyword_patterns')
                let g:neocomplete#keyword_patterns = {}
                endif
               let g:neocomplete#keyword_patterns['default'] = '\h\w*'

               " Plugin key-mappings.
               inoremap <expr><C-g>     neocomplete#undo_completion()
               inoremap <expr><C-l>     neocomplete#complete_common_string()

               " Recommended key-mappings.
               " <CR>: close popup and save indent.
               inoremap <silent> <CR> <C-r>=<SID>my_cr_function()<CR>
               function! s:my_cr_function()
                     return neocomplete#close_popup() . "\<CR>"
                       " For no inserting <CR> key.
                         "return pumvisible() ? neocomplete#close_popup() :
                         "\<CR>"
               endfunction
               " <TAB>: completion.
               inoremap <expr><TAB>  pumvisible() ? "\<C-n>" : "\<TAB>"
               " <C-h>, <BS>: close popup and delete backword char.
               inoremap <expr><C-h>
               " neocomplete#smart_close_popup()."\<C-h>"
               inoremap <expr><BS> neocomplete#smart_close_popup()."\<C-h>"
               inoremap <expr><C-y>  neocomplete#close_popup()
               inoremap <expr><C-e>  neocomplete#cancel_popup()
               " Close popup by <Space>.
               "inoremap <expr><Space> pumvisible() ?
               " neocomplete#close_popup() : "\<Space>"
               
               " For cursor moving in insert mode(Not recommended)
               " inoremap <expr><Left>  neocomplete#close_popup() .
               "\<Left>"
               " inoremap <expr><Right> neocomplete#close_popup() .
               "\<Right>"
               " inoremap <expr><Up>    neocomplete#close_popup() . "\<Up>"
               " inoremap <expr><Down>  neocomplete#close_popup() .
               "\<Down>"
               " Or set this.
               " let g:neocomplete#enable_cursor_hold_i = 1
               " Or set this.
               " let g:neocomplete#enable_insert_char_pre = 1
               
               " AutoComplPop like behavior.
               " let g:neocomplete#enable_auto_select = 1
               
               " Shell like behavior(not recommended).
               set completeopt+=longest
               let g:neocomplete#enable_auto_select = 1
               " let g:neocomplete#disable_auto_complete = 1
               " inoremap <expr><TAB>  pumvisible() ? "\<Down>" : \<C-x>\<C-u>"
               
               " Enable omni completion.
               autocmd FileType css setlocal omnifunc=csscomplete#CompleteCSS
               autocmd FileType html,markdown setlocal   omnifunc=htmlcomplete#CompleteTags
               " autocmd FileType javascript setlocal  omnifunc=javascriptcomplete#CompleteJS
               " autocmd FileType python setlocal      omnifunc=pythoncomplete#Complete
               " autocmd FileType xml setlocal     omnifunc=xmlcomplete#CompleteTags
               
               " Enable heavy omni completion.
               if !exists('g:neocomplete#sources#omni#input_patterns')
                     let g:neocomplete#sources#omni#input_patterns = {}
                endif
                let g:neocomplete#sources#omni#input_patterns.php = '[^\t]->\h\w*\|\h\w*::'
                let g:neocomplete#sources#omni#input_patterns.c =  '[^.[:digit:] *\t]\%(\.\|->\)'
                let g:neocomplete#sources#omni#input_patterns.cpp = '[^.[:digit:] *\t]\%(\.\|->\)\|\h\w*::'
            " "}

        " Misc {
                :map <C-F10> <Esc>:vsp<CR>:VTree<CR>
                " map Control + F10 to Vtree


                let g:NERDShutUp=1
                let b:match_ignorecase = 1
        " }

        " ShowMarks {
                let showmarks_include = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
                " Don't leave on by default, use :ShowMarksOn to enable
                let g:showmarks_enable = 0
                " For marks a-z
                highlight ShowMarksHLl gui=bold guibg=LightBlue guifg=Blue
                " For marks A-Z
                highlight ShowMarksHLu gui=bold guibg=LightRed guifg=DarkRed
                " For ale other marks
                highlight ShowMarksHLo gui=bold guibg=LightYellow guifg=DarkYellow
                " For multiple marks on the same line.
                highlight ShowMarksHLm gui=bold guibg=LightGreen guifg=DarkGreen
        " }


            "Gundo
            map <leader>u :GundoToggle<CR>
            "}



        " Ctags {
    " This will look in the current directory for 'tags', and work up the tree towards root until one is found.
                set tags=./tags;/,$HOME/.vim/tags
                map <C-\> :tab split<CR>:exec("tag ".expand("<cword>"))<CR> " C-\ - Open the definition in a new tab
                map <A-]> :vsp <CR>:exec("tag ".expand("<cword>"))<CR>      " A-] - Open the definition in a vertical split
                set tags+=~/.vim/tags/boost
                set tags+=~/.vim/tags/libc6
                set tags+=~/.vim/tags/stdlibcpp
        " }


        " Delimitmate {
                au FileType * let b:delimitMate_autoclose = 1

                " If using html auto complete (complete closing tag)
                au FileType xml,html,xhtml let b:delimitMate_matchpairs = "(:),[:],{:}"
        " }

        " UltiSnipMate {
        let g:UltiSnipsEditSplit='horizontal'
        " let g:UltiSnipsExpandTrigger="<tab>"
        " let g:UltiSnipsJumpForwardTrigger="<tab>"
        " let g:UltiSnipsJumpBackwardTrigger="<s-tab>"
        " let g:UltiSnipsListSnippets="<c-tab>"
        " }

        " NerdTree {
                map <C-e> :NERDTreeToggle<CR>:NERDTreeMirror<CR>
                map <leader>e :NERDTreeFind<CR>
                nmap <leader>nt :NERDTreeFind<CR>

                let NERDTreeShowBookmarks=1
                let NERDTreeIgnore=['\.pyc', '\~$', '\.swo$', '\.swp$', '\.git', '\.hg', '\.svn', '\.bzr']
                let NERDTreeChDirMode=1
                let NERDTreeQuitOnOpen=1
                let NERDTreeShowHidden=1
                " let NERDTreeKeepTreeInNewTab=1
               " " autocmd VimEnter * NERDTree
                " autocmd VimEnter * wincmd p
        " }

    " Tabularize {
        if exists(":Tabularize")
          nmap <Leader>f= :Tabularize /=<CR>
          vmap <Leader>f= :Tabularize /=<CR>
          nmap <Leader>f: :Tabularize /:<CR>
          vmap <Leader>f: :Tabularize /:<CR>
          nmap <Leader>f:: :Tabularize /:\zs<CR>
          vmap <Leader>f:: :Tabularize /:\zs<CR>
          nmap <Leader>f, :Tabularize /,<CR>
          vmap <Leader>f, :Tabularize /,<CR>
          nmap <Leader>f<Bar> :Tabularize /<Bar><CR>
          vmap <Leader>f<Bar> :Tabularize /<Bar><CR>
        endif
     " }


                " Unite {
                        map <C-b> :CtrlPBuffer<CR>
                        let g:ctrlp_extensions = ['smarttabs']

                        let g:ctrlp_custom_ignore = 'node_modules\|report\|vendor\|cache\|git'
                        nnoremap <space>/ :Unite -no-quit -buffer-name=search grep:.<cr>

                " Taglist Variables {
                        let Tlist_Auto_Highlight_Tag = 1
                        let Tlist_Auto_Update = 1
                        let Tlist_Exit_OnlyWindow = 1
                        let Tlist_File_Fold_Auto_Close = 1
                        let Tlist_Highlight_Tag_On_BufEnter = 1
                        let Tlist_Use_Right_Window = 1
                        let Tlist_Use_SingleClick = 1

                        let g:ctags_statusline=1
                        " Override how taglist does javascript
                        let g:tlist_javascript_settings = 'javascript;f:function;c:class;m:method;p:property;v:global'
                 " }

                " JSON {
                        nmap <leader>jt <Esc>:%!python -m json.tool<CR><Esc>:set filetype=json<CR>
                 " }

                 let g:vimfiler_as_default_explorer = 1
                 let g:vimfiler_enable_clipboard = 0
                 let g:vimfiler_safe_mode_by_default = 0
                 " let g:vimfiler_edit_action = 'tabopen'

                 nmap <Leader>f [vfiler]
                 nnoremap [vfiler] <nop>
                 nnoremap <silent>   [vfiler]f   :VimFiler<CR>
                 nnoremap <silent>   [vfiler]t   :VimFilerTab<CR>
                 nnoremap <silent>   [vfiler]e   :VimFilerExplorer<CR>
                 nnoremap<silent> <F3> :<C-u>VimFilerExplorer<CR>
                 nnoremap [vfiler]b :VimFilerBufferDir<CR>

        " }
" }

" GUI Setting {
        " GVIM- (here instead of .gvimrc)
        if has('gui_running')
                set guioptions-=T               " remove the toolbar
                set lines=40                    " 40 lines of text instead of 24,
        else
                set term=builtin_ansi       " Make arrow and other keys work
        endif
" }

function! InitializeDirectories()
  let separator = "."
  let parent = $HOME
  let prefix = '.vim'
  let dir_list = {
                          \ 'backup': 'backupdir',
                          \ 'views': 'viewdir',
                          \ 'swap': 'directory',
                          \ 'undo': 'undodir' }

  for [dirname, settingname] in items(dir_list)
          let directory = parent . '/' . prefix . dirname . "/"
          if exists("*mkdir")
                  if !isdirectory(directory)
                  call mkdir(directory)
                  endif
          endif
          if !isdirectory(directory)
                  echo "Warning: Unable to create backup directory: " . directory
                  echo "Try: mkdir -p " . directory
          else
          let directory = substitute(directory, " ", "\\\\ ", "")
          exec "set " . settingname . "=" . directory
          endif
  endfor
endfunction
call InitializeDirectories()

function! NERDTreeInitAsNeeded()
    redir => bufoutput
    buffers!
    redir END
    let idx = stridx(bufoutput, "NERD_tree")
    if idx > -1
        NERDTreeMirror
        NERDTreeFind
        wincmd l
    endif
endfunction

command! -complete=shellcmd -nargs=+ Shell call s:RunShellCommand(<q-args>)
function! s:RunShellCommand(cmdline)
  echo a:cmdline
  let expanded_cmdline = a:cmdline
  for part in split(a:cmdline, ' ')
     if part[0] =~ '\v[%#<]'
        let expanded_part = fnameescape(expand(part))
        let expanded_cmdline = substitute(expanded_cmdline, part, expanded_part, '')
     endif
  endfor
  botright new
  setlocal buftype=nofile bufhidden=wipe nobuflisted noswapfile nowrap
  call setline(1, 'You entered:    ' . a:cmdline)
  call setline(2, 'Expanded Form:  ' .expanded_cmdline)
  call setline(3,substitute(getline(2),'.','=','g'))
  execute '$read !'. expanded_cmdline
  setlocal nomodifiable
  1
endfunction


" Use local vimrc if available {
    if filereadable(expand("~/.vimrc.local"))
        source ~/.vimrc.local
    endif
" }
   let g:UltiSnipsUsePythonVersion = 2
        

set term=screen-256color
