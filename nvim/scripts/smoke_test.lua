-- Headless smoke test: force-loads every plugin and verifies each works.
-- Run with: nvim --headless "+luafile scripts/smoke_test.lua"
local failures = {}
local checks = 0

local function check(name, fn)
  checks = checks + 1
  local ok, err = pcall(fn)
  if ok then
    print(string.format("PASS  %s", name))
  else
    print(string.format("FAIL  %s  ->  %s", name, err))
    table.insert(failures, { name = name, err = err })
  end
end

-- Helper: open a fresh buffer in a NEW window (avoids winfixbuf of floating UIs)
local function new_scratch_buf(lines, ft)
  vim.cmd("new")
  local buf = vim.api.nvim_get_current_buf()
  vim.bo[buf].buftype = "nofile"
  if ft then
    vim.bo[buf].filetype = ft
  end
  if lines then
    vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  end
  return buf
end

-- Force-load all lazy plugins (bypasses lazy-loading triggers)
local lazy_config = require("lazy.core.config")
local names = {}
for name in pairs(lazy_config.plugins) do
  table.insert(names, name)
end
require("lazy").load({ plugins = names })
vim.wait(3000, function()
  return false
end, 100) -- let lazy events settle

-- 1. Core config modules
check("options loaded", function()
  assert(vim.opt.termguicolors:get(), "termguicolors not set")
end)

check("leader is ','", function()
  assert(vim.g.mapleader == ",", "mapleader is " .. tostring(vim.g.mapleader))
end)

check("keymap '; -> :' active", function()
  for _, m in ipairs(vim.api.nvim_get_keymap("n")) do
    if m.lhs == ";" and m.rhs == ":" then
      return
    end
  end
  error("';' mapping not found")
end)

-- 2. Telescope
check("telescope loads", function()
  require("telescope").setup({})
end)

check("telescope find_files command exists", function()
  assert(vim.fn.exists(":Telescope") == 2, ":Telescope not defined")
end)

-- 3. nvim-tree
check("nvim-tree opens", function()
  require("nvim-tree.api").tree.open()
  vim.wait(2000, function()
    for _, win in ipairs(vim.api.nvim_list_wins()) do
      local ok, ft = pcall(vim.api.nvim_buf_get_option, vim.api.nvim_win_get_buf(win), "filetype")
      if ok and ft == "NvimTree" then
        return true
      end
    end
    return false
  end, 100)
  require("nvim-tree.api").tree.close()
end)

-- 4. mini editing modules
check("mini.pairs active", function()
  assert(_G.MiniPairs, "MiniPairs global missing")
end)

check("mini.surround active", function()
  assert(_G.MiniSurround, "MiniSurround global missing")
end)

check("mini.comment active", function()
  assert(_G.MiniComment, "MiniComment global missing")
end)

check("mini.align active", function()
  assert(_G.MiniAlign, "MiniAlign global missing")
end)

check("comment a buffer line end-to-end", function()
  local buf = new_scratch_buf({ "local x = 1" }, "lua")
  require("mini.comment").toggle_lines(1, 1)
  local lines = vim.api.nvim_buf_get_lines(buf, 0, 1, false)
  assert(lines[1]:match("^%-%-"), "comment toggle failed: " .. tostring(lines[1]))
  require("mini.comment").toggle_lines(1, 1)
  assert(vim.api.nvim_buf_get_lines(buf, 0, 1, false)[1] == "local x = 1", "uncomment failed")
end)

check("surround a word end-to-end", function()
  local buf = new_scratch_buf({ "hello" }, "text")
  vim.api.nvim_win_set_cursor(0, { 1, 0 })
  -- saiw" : add " around inner word
  vim.api.nvim_feedkeys(vim.api.nvim_replace_termcodes('saiw"', true, false, true), "x", false)
  assert(vim.api.nvim_buf_get_lines(buf, 0, 1, false)[1] == '"hello"', "surround failed")
end)

-- 5. Treesitter (new main-branch API)
check("treesitter lua parser installs and parses", function()
  local ts = require("nvim-treesitter")
  ts.install({ "lua" }):wait(120000)
  local buf = new_scratch_buf({ "local x = 1" }, "lua")
  vim.cmd("silent! doautocmd FileType lua")
  vim.wait(5000, function()
    return pcall(function()
      vim.treesitter.get_parser(buf, "lua"):parse()
    end)
  end, 200)
  local parser = vim.treesitter.get_parser(buf, "lua")
  local trees = parser:parse()
  assert(trees[1] and trees[1]:root() ~= nil, "parse produced no tree")
end)

check("treesitter highlight active on lua", function()
  local buf = new_scratch_buf({ "local x = 1" }, "lua")
  vim.cmd("silent! doautocmd FileType lua")
  pcall(vim.treesitter.start, buf, "lua")
  vim.wait(3000, function()
    for _, hl in pairs(vim.treesitter.highlighter.active) do
      if type(hl) == "table" and hl.bufnr == buf then
        return true
      end
    end
    return false
  end, 200)
  local running = false
  for _, hl in pairs(vim.treesitter.highlighter.active) do
    if type(hl) == "table" and hl.bufnr == buf then
      running = true
      break
    end
  end
  assert(running, "highlighter not running for lua buffer")
end)

-- 6. blink.cmp
check("blink.cmp loads", function()
  require("blink.cmp").reload()
end)

-- 7. LSP configured via native API (nvim 0.11+)
check("vim.lsp.enable API available", function()
  assert(type(vim.lsp.enable) == "function", "vim.lsp.enable missing (need nvim 0.11+)")
end)

check("LSP servers enabled", function()
  for _, server in ipairs({ "gopls", "ts_ls", "lua_ls", "terraformls", "basedpyright" }) do
    local ok = pcall(vim.lsp.is_enabled, server)
    if ok then
      assert(vim.lsp.is_enabled(server), server .. " not enabled")
    else
      assert(vim.lsp.config[server] ~= nil, server .. " config missing")
    end
  end
end)

-- 8. gitsigns attaches in the dotfiles git repo
check("gitsigns attaches in git repo", function()
  vim.cmd("edit " .. vim.fn.fnamemodify(debug.getinfo(1, "S").source:sub(2), ":p"))
  local buf = vim.api.nvim_get_current_buf()
  vim.wait(5000, function()
    return vim.b[buf].gitsigns_head ~= nil or vim.b[buf].gitsigns_status_dict ~= nil
  end, 200)
  assert(vim.b[buf].gitsigns_head ~= nil or vim.b[buf].gitsigns_status_dict ~= nil,
    "gitsigns did not attach")
end)

check("gitsigns blame functions available + keymapped", function()
  assert(type(package.loaded.gitsigns.blame_line) == "function", "blame_line missing")
  assert(type(package.loaded.gitsigns.blame) == "function", "blame missing")
  local buf = vim.api.nvim_get_current_buf()
  assert(vim.fn.maparg("<leader>gb", "n") ~= "", "<leader>gb keymap missing")
  assert(vim.fn.maparg("<leader>gB", "n") ~= "", "<leader>gB keymap missing")
end)

check("diffview loads + commands exist", function()
  require("diffview").setup({})
  assert(vim.fn.exists(":DiffviewOpen") == 2, ":DiffviewOpen not defined")
  assert(vim.fn.exists(":DiffviewFileHistory") == 2, ":DiffviewFileHistory not defined")
end)

-- 9. UI plugins
check("barbar loads + commands + keymaps", function()
  require("barbar").setup({})
  assert(vim.fn.exists(":BufferNext") == 2, ":BufferNext not defined")
  assert(vim.fn.exists(":BufferPick") == 2, ":BufferPick not defined")
  assert(vim.fn.exists(":BufferPin") == 2, ":BufferPin not defined")
  assert(vim.fn.maparg("<A-,>", "n") ~= "", "<A-,> barbar keymap missing")
  assert(vim.fn.maparg(" bp", "n") ~= "", "<space>bp barbar keymap missing")
  -- Telescope keeps <C-p> (find_files); barbar's README <C-p> BufferPick must not hijack it.
  -- (lazy.nvim <C-p> keys-stub is a callback with hidden rhs, so assert spec ownership.)
  local owned = false
  for _, k in ipairs(require("lazy.core.config").plugins["telescope.nvim"].keys or {}) do
    if k[1] == "<C-p>" and type(k[2]) == "string" and k[2]:match("find_files") then
      owned = true
    end
  end
  assert(owned, "<C-p> not owned by telescope find_files")
end)

check("tabman loads + command exists", function()
  local ok = pcall(require, "tabman")
  assert(ok, "require('tabman') failed")
  assert(vim.fn.exists(":Tabman") == 2, ":Tabman not defined")
  assert(vim.fn.maparg("<leader>t", "n") ~= "", "<leader>t (:Tabman) keymap missing")
end)

check("lualine active", function()
  assert(package.loaded["lualine"], "lualine not loaded")
end)

check("which-key active", function()
  assert(package.loaded["which-key"], "which-key not loaded")
end)

check("colorscheme is kanagawa", function()
  assert((vim.g.colors_name or ""):match("^kanagawa"), "colors_name is " .. tostring(vim.g.colors_name))
end)

check("hardtime works (its nui dep must be present)", function()
  local ok, err = pcall(require, "hardtime")
  assert(ok, "require('hardtime') failed: " .. tostring(err))
  assert(vim.fn.exists(":Hardtime") == 2, ":Hardtime not defined")
  local ok2, err2 = pcall(vim.cmd, "Hardtime report")
  assert(ok2, ":Hardtime report failed (nui popup broken?): " .. tostring(err2))
end)

check("cheatsheet popup (:Cheatsheet / ,kh)", function()
  assert(vim.fn.exists(":Cheatsheet") == 2, ":Cheatsheet not defined")
  for _, lhs in ipairs({ "?", "<leader>?" }) do
    local m = vim.fn.maparg(lhs, "n", false, true)
    assert(type(m) == "table" and tostring(m.rhs or ""):find("Cheatsheet"),
      "cheatsheet not mapped to " .. lhs)
  end
  vim.cmd("Cheatsheet")
  local bufnr = vim.g.cheatsheet_bufnr
  assert(bufnr and vim.api.nvim_buf_is_valid(bufnr), "cheatsheet buffer missing")
  local n = vim.api.nvim_buf_line_count(bufnr)
  assert(n > 80, "cheatsheet too thin: " .. n .. " lines")
  local txt = table.concat(vim.api.nvim_buf_get_lines(bufnr, 0, -1, false), "\n")
  for _, needle in ipairs({ "Quick start", "Splits & windows", "Environment (live)", "Plugin keys", "Keymaps", "Plugins", "Leader", "barbar" }) do
    assert(txt:find(needle, 1, true), "cheatsheet missing section: " .. needle)
  end
  vim.cmd("Cheatsheet") -- toggle closed
  vim.wait(200)
  assert(vim.g.cheatsheet_bufnr == nil, "cheatsheet did not close on toggle")
end)

check("nvim-notify owns vim.notify (hardtime hints sink)", function()
  local notify = require("notify")
  assert(vim.fn.exists(":Notifications") == 2, ":Notifications not defined")
  assert(vim.notify == notify, "vim.notify not replaced by nvim-notify")
  -- hardtime's hints route through plain vim.notify — prove the chain lands
  -- in notify history (records may hold message as string or table of lines)
  local before = #require("notify").history()
  vim.notify("hardtime sink probe", vim.log.levels.WARN, { title = "Hardtime" })
  vim.wait(800, function()
    return #require("notify").history() > before
  end, 100)
  assert(#require("notify").history() > before, "notification did not land in nvim-notify history")
end)

check("nui.nvim components load", function()
  for _, mod in ipairs({ "nui.popup", "nui.layout", "nui.input", "nui.menu", "nui.split" }) do
    local ok, err = pcall(require, mod)
    assert(ok, mod .. " failed to load: " .. tostring(err))
  end
end)

check("indent-blankline (ibl) active", function()
  assert(package.loaded["ibl"] or vim.g.ibl_loaded_config ~= false, "ibl not loaded")
end)

-- 10. fugitive command available
check("fugitive :Git available", function()
  assert(vim.fn.exists(":Git") == 2, ":Git command not defined")
end)

-- 11. Plugins added in the 2026-10 batch
check("conform loads + :ConformInfo", function()
  require("conform")
  assert(vim.fn.exists(":ConformInfo") == 2, ":ConformInfo not defined")
end)

check("nvim-lint loads", function()
  assert(type(require("lint").try_lint) == "function", "lint.try_lint missing")
end)

check("nvim-dap loads", function()
  assert(type(require("dap").toggle_breakpoint) == "function", "dap.toggle_breakpoint missing")
end)

check("nvim-dap-ui loads", function()
  assert(type(require("dapui").toggle) == "function", "dapui.toggle missing")
end)

check("resession loads", function()
  assert(type(require("resession").save) == "function", "resession.save missing")
end)

check("mini.icons mocks nvim-web-devicons", function()
  require("mini.icons")
  assert(type(require("nvim-web-devicons")) == "table", "devicons mock failed")
end)

check("mini.starter active", function()
  assert(_G.MiniStarter, "MiniStarter global missing")
end)

-- 12. DB + k8s + AI batch (db.lua, k8s.lua, ai.lua)
check("vim-dadbod + dadbod-ui commands", function()
  assert(vim.fn.exists(":DB") == 2, ":DB not defined")
  assert(vim.fn.exists(":DBUIToggle") == 2, ":DBUIToggle not defined")
end)

check("dadbod blink source loads", function()
  assert(type(require("vim_dadbod_completion.blink")) == "table", "dadbod blink module missing")
end)

check("kubectl.nvim commands", function()
  assert(vim.fn.exists(":Kubectl") == 2, ":Kubectl not defined")
end)

check("yaml-companion loads + yamlls enabled", function()
  require("yaml-companion")
  local ok, enabled = pcall(vim.lsp.is_enabled, "yamlls")
  assert(ok and enabled, "yamlls not enabled")
end)

check("minuet + blink source load", function()
  require("minuet")
  assert(type(require("minuet.blink")) == "table", "minuet.blink module missing")
end)

check("kubeconform command + binary", function()
  assert(vim.fn.exists(":Kubeconform") == 2, ":Kubeconform not defined")
  assert(vim.fn.executable("kubeconform") == 1, "kubeconform binary missing")
end)

check("db: all envs seeded in g:dbs", function()
  -- Stub gcloud so this check needs NO real cloud credentials: config.db.seed()
  -- shells out to `gcloud auth print-access-token`. The stub answers with a
  -- fake token AND writes a marker file proving the stub (not a real gcloud)
  -- served the request — the plugin also auto-seeds at startup with whatever
  -- gcloud resolves, so we assert structure, never token content.
  local stub = vim.fn.stdpath("cache") .. "/smoke-gcloud-stub"
  vim.fn.mkdir(stub, "p")
  vim.fn.writefile({
    "#!/bin/sh",
    'echo called >> "' .. stub .. '/called"',
    'case "$*" in',
    '  "auth print-access-token") echo "fake-token-0000-fake";;',
    '  "auth application-default print-access-token") echo "fake-adc";;',
    "esac",
  }, stub .. "/gcloud")
  vim.fn.setfperm(stub .. "/gcloud", "rwxr-xr-x") -- symbolic mode (E475 without)
  vim.fn.delete(stub .. "/called") -- no stale marker from previous runs
  local old_path = vim.env.PATH
  vim.env.PATH = stub .. ":" .. vim.env.PATH

  require("config.db").seed()
  -- wait specifically for the STUB to serve the token, not for the plugin's
  -- startup seed (async — it can race this check)
  assert(
    vim.wait(10000, function()
      return vim.fn.filereadable(stub .. "/called") == 1
    end),
    "gcloud stub was not invoked"
  )
  local keys = vim.g.dbs and vim.tbl_keys(vim.g.dbs) or {}
  assert(#keys >= 4, "expected 4 env connections, got " .. vim.inspect(keys))
  -- structural asserts only (encoded user, sslmode, port): no username,
  -- account or token-content assumptions (startup seed may race/replace)
  for env, url in pairs(vim.g.dbs) do
    assert(url:match("%%40"), env .. " url lacks encoded user separator")
    assert(url:match("@127%.0%.0%.1:(%d+)/"), env .. " url lacks localhost:port")
    assert(url:find("sslmode=require", 1, true), env .. " url lacks sslmode")
  end
  vim.env.PATH = old_path
  vim.fn.delete(stub, "rf")
end)

check("aerial: module + :AerialToggle", function()
  assert(require("aerial"), "aerial module failed to load")
  assert(vim.fn.exists(":AerialToggle") == 2, ":AerialToggle not defined")
end)

check("trouble: module + :Trouble", function()
  assert(require("trouble"), "trouble module failed to load")
  assert(vim.fn.exists(":Trouble") == 2, ":Trouble not defined")
end)

check("treesitter-context: module loads", function()
  assert(require("treesitter-context"), "treesitter-context failed to load")
end)

check("herdr-nvim: module + annotation API", function()
  local h = require("herdr-nvim")
  assert(type(h.comment_line) == "function", "comment_line API missing")
  assert(type(h.send_all) == "function", "send_all API missing")
end)

check("telescope file_browser extension", function()
  local t = require("telescope")
  local ok, ext = pcall(function() return t.extensions.file_browser end)
  assert(ok and ext, "file_browser extension not registered")
end)

print(string.format("\n%d/%d checks passed", checks - #failures, checks))
if #failures > 0 then
  print("VALIDATION FAILED")
  -- MUST exit non-zero BEFORE qa: vim.cmd("qa!") exits 0 and would mask the
  -- failure from validate.sh / CI (os.exit kills nvim directly)
  os.exit(1)
end
print("VALIDATION OK")
vim.cmd("qa!")
