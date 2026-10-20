-- Explorer commands for neo-tree: one tree window, two scopes.
--   ,e  local explorer rooted at the current file's parent directory,
--       revealing + selecting the file (GoLand-style)
--   ,E  worktree explorer rooted at the git worktree root (.git dir or file)
-- Re-pressing either closes the tree (no duplicate windows).
local M = {}

local function tree_is_open()
  for _, win in ipairs(vim.api.nvim_list_wins()) do
    local ft = vim.bo[vim.api.nvim_win_get_buf(win)].filetype or ""
    if ft:match("^neo%-tree") then return true end
  end
  return false
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
  if tree_is_open() then
    run({ action = "close", position = "left" })
    return
  end
  local ctx = require("config.context")
  local file = ctx.source_file()
  run({
    action = "focus",
    source = "filesystem",
    position = "left",
    dir = ctx.source_dir(),
    reveal_file = file or nil,
    reveal = file ~= nil,
  })
end

---,E — root at the git worktree root.
function M.toggle_worktree()
  if tree_is_open() then
    run({ action = "close", position = "left" })
    return
  end
  run({
    action = "focus",
    source = "filesystem",
    position = "left",
    dir = require("config.context").worktree_root(),
  })
end

return M