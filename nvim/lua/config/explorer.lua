-- Explorer commands for neo-tree: one tree window, two scopes.
--   ,e  local explorer rooted at the current file's parent directory,
--       revealing + selecting the file (GoLand-style)
--   ,E  worktree explorer rooted at the git worktree root (.git dir or file)
--   <C-n> toggle at the worktree root (old NERDTree/nvim-tree key)
--
-- Uses neo-tree's own `toggle` flag: command.execute() runs renderer.close()
-- SYNCHRONOUSLY and falls through to open only when nothing was closed.
-- The previous window-scan + action="close" dance raced with neo-tree's
-- async close: a fast <C-n><C-n><C-n> saw the closing window as still open
-- and closed an already-closing tree — 3rd press "no effect".
local M = {}

-- Debounce: neo-tree's OPEN path is deferred internally (while close is
-- synchronous), so a sub-100ms key mash could get a press absorbed mid-open.
-- Collapsing rapid presses into one toggle keeps parity exact for humans.
local pending_timer = nil
local function debounced(fn)
  return function(...)
    local args = { ... }
    if pending_timer then
      vim.fn.timer_stop(pending_timer)
    end
    pending_timer = vim.fn.timer_start(100, function()
      pending_timer = nil
      fn(unpack(args))
    end)
  end
end

local function run(args)
  local ok, cmd = pcall(require, "neo-tree.command")
  if not ok then
    vim.notify("explorer: neo-tree not loaded", vim.log.levels.ERROR)
    return
  end
  -- neo-tree's focus/show with a dir sets the GLOBAL cwd to the tree root;
  -- that would silently re-scope every picker — restore the caller's cwd.
  local cwd = vim.fn.getcwd()
  cmd.execute(args)
  if vim.fn.getcwd() ~= cwd then
    vim.fn.chdir(cwd)
  end
end

---,e — root at the current file's directory, file revealed & selected.
function M.toggle_local()
  return debounced(function()
    local ctx = require("config.context")
    local file = ctx.source_file()
    run({
      action = "focus",
      toggle = true,
      source = "filesystem",
      position = "left",
      dir = ctx.source_dir(),
      reveal_file = file or nil,
      reveal = file ~= nil,
    })
  end)()
end

---,E — root at the git worktree root.
function M.toggle_worktree()
  return debounced(function()
    run({
      action = "focus",
      toggle = true,
      source = "filesystem",
      position = "left",
      dir = require("config.context").worktree_root(),
    })
  end)()
end

return M
