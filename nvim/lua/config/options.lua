-- Options ported from the old .vimrc
local opt = vim.opt

-- (netrw left enabled: nvim-tree hijacks it itself; hard-disabling caused E216)

-- UI
opt.background = "dark"
opt.termguicolors = true
opt.number = true
opt.relativenumber = true
opt.cursorline = true
opt.showmode = false -- lualine shows it
opt.laststatus = 3
opt.scrolloff = 3
opt.showmatch = true
opt.list = true
opt.listchars = { tab = ">.", trail = ".", extends = "#", nbsp = "." }
opt.winminheight = 0
opt.wildmenu = true
opt.wildmode = "list:longest,full"
opt.tabpagemax = 15
opt.mouse = "a"

-- Search
opt.incsearch = true
opt.hlsearch = true
opt.ignorecase = true
opt.smartcase = true
opt.gdefault = true

-- Indentation / formatting
opt.autoindent = true
opt.smarttab = true
opt.expandtab = true
opt.shiftwidth = 4
opt.tabstop = 4
opt.softtabstop = 4
opt.wrap = true
opt.whichwrap:append("b,s,h,l,<,>,[,]")
opt.comments = "sl:/*,mb:*,elx:*/"

-- Behaviour
opt.autoread = true
opt.backspace = "indent,eol,start"
opt.virtualedit = "onemore"
opt.history = 1000
opt.shortmess:append("filmnrxoOtT")
opt.undofile = true
opt.undolevels = 1000
opt.undoreload = 10000
opt.swapfile = false
opt.backup = false -- replaced by persistent undofile
opt.updatetime = 250 -- faster gitgutter/diagnostics
opt.timeoutlen = 400

-- Completion
opt.completeopt = { "menu", "menuone", "noselect" }

-- View persistence (folds/cursor), like the old mkview/loadview autocmds
opt.viewoptions = "folds,cursor,unix,slash"
vim.api.nvim_create_autocmd("BufWinLeave", {
  pattern = "?*",
  callback = function(args)
    if vim.bo[args.buf].buflisted and vim.bo[args.buf].buftype == "" then
      pcall(vim.cmd.mkview)
    end
  end,
})
vim.api.nvim_create_autocmd("BufWinEnter", {
  pattern = "?*",
  callback = function(args)
    if vim.bo[args.buf].buflisted and vim.bo[args.buf].buftype == "" then
      pcall(vim.cmd.loadview)
    end
  end,
})

-- Relative number toggling (from old vimrc)
vim.api.nvim_create_autocmd({ "BufEnter", "FocusGained", "InsertLeave" }, {
  pattern = "*",
  command = "set relativenumber",
})
vim.api.nvim_create_autocmd({ "BufLeave", "FocusLost", "InsertEnter" }, {
  pattern = "*",
  command = "set norelativenumber",
})

-- Strip trailing whitespace on save for common source files
vim.api.nvim_create_autocmd("FileType", {
  pattern = { "c", "cpp", "java", "php", "javascript", "typescript", "python", "yaml", "json", "sh" },
  callback = function(args)
    if vim.b[args.buf].ws_strip_autocmd then
      return
    end
    vim.b[args.buf].ws_strip_autocmd = true
    vim.api.nvim_create_autocmd("BufWritePre", {
      buffer = args.buf,
      command = [[silent! keepjumps keeppatterns %s/\s\+$//e]],
    })
  end,
})

-- Highlight trailing whitespace (real editable buffers only).
-- GUARD: this must NOT apply to float popups — blink.cmp's menu pads every
-- line with trailing spaces to the full column width, so `\s\+$` matched the
-- entire padding and painted solid red rows over the completion menu.
vim.cmd([[highlight ExtraWhitespace ctermbg=red guibg=red]])
vim.api.nvim_create_autocmd("BufWinEnter", {
  pattern = "*",
  callback = function(args)
    if vim.bo[args.buf].buftype ~= "" then return end -- skip floats/scratch/help
    if vim.bo[args.buf].filetype:find("^blink%-cmp") then return end
    vim.fn.matchadd("ExtraWhitespace", [[\s\+$]])
  end,
})
-- Float windows inherit matches when a normal buffer's window opens them
-- (blink/nui reuse windows); strip them defensively on any float that appears.
vim.api.nvim_create_autocmd("WinEnter", {
  callback = function()
    local cfg = vim.api.nvim_win_get_config(0)
    if cfg.relative ~= "" then
      for _, m in ipairs(vim.fn.getmatches(0)) do
        if m.group == "ExtraWhitespace" then vim.fn.matchdelete(m.id, 0) end
      end
    end
  end,
})

-- Data dirs (replaces old InitializeDirectories(): backup/swap/undo/views)
for _, name in ipairs({ "backup", "undo", "view", "swap" }) do
  local dir = vim.fn.stdpath("state") .. "/" .. name
  if vim.fn.isdirectory(dir) == 0 then
    vim.fn.mkdir(dir, "p")
  end
end
opt.undodir = vim.fn.stdpath("state") .. "/undo//"
opt.backupdir = vim.fn.stdpath("state") .. "/backup//"
opt.viewdir = vim.fn.stdpath("state") .. "/view//"
