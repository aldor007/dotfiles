-- Context helpers: "where am I working" for the explorer + Telescope scoping.
-- Source-dir search roots follow the LAST REAL source file, so moving focus
-- into a sidebar (neo-tree/telescope) never loses the working context.
local M = {}

-- utility buffers that must never be treated as the search context
local UTILITY_FT = {
  ["neo-tree"] = true, ["neo-tree-popup"] = true, ["NvimTree"] = true,
  ["telescope"] = true, ["prompt"] = true, ["qf"] = true, ["help"] = true,
  ["man"] = true, ["lazy"] = true, ["notify"] = true, ["spectre"] = true,
  ["git"] = true, ["fugitive"] = true, ["codecompanion"] = true,
}
local UTILITY_BT = {
  nofile = true, quickfix = true, help = true, prompt = true,
  popup = true, terminal = true, acwrite = true,
}

local last_source_file

local function is_utility()
  local buf = vim.api.nvim_get_current_buf()
  return UTILITY_FT[vim.bo[buf].filetype] or UTILITY_BT[vim.bo[buf].buftype]
end

-- absolute path + real file (not a dir, not a name without a path)
local function is_source_file(path)
  if type(path) ~= "string" or path == "" then return false end
  if not (vim.startswith(path, "/") or vim.startswith(path, "~")) then return false end
  if vim.fn.isdirectory(path) == 1 then return false end
  local ok, stat = pcall(vim.loop.fs_stat, vim.fn.expand(path))
  return ok and stat and stat.type == "file"
end

---Remember the last real source file so utility windows (tree, pickers) never
---lose the context. Cheap BufEnter hook; call once from init.lua.
function M.setup()
  vim.api.nvim_create_autocmd("BufEnter", {
    callback = function()
      if is_utility() then return end
      local path = vim.api.nvim_buf_get_name(vim.api.nvim_get_current_buf())
      if is_source_file(path) then
        last_source_file = path
      end
    end,
  })
end

---Current source FILE (nil when focus is in a utility buffer).
function M.source_file()
  local buf = vim.api.nvim_get_current_buf()
  if UTILITY_FT[vim.bo[buf].filetype] or UTILITY_BT[vim.bo[buf].buftype] then
    return last_source_file
  end
  local cur = vim.api.nvim_buf_get_name(buf)
  if is_source_file(cur) then return cur end
  return last_source_file
end

---Directory to scope searches to: parent of the last real source file.
---Falls back: current directory buffer -> the buffer's dir -> cwd.
function M.source_dir()
  local file = M.source_file()
  if file then
    local d = vim.fn.fnamemodify(file, ":h")
    if d ~= "" then return d end
  end
  -- a directory buffer (`:e .`) is itself a usable root
  local buf = vim.api.nvim_get_current_buf()
  local cur = vim.api.nvim_buf_get_name(buf)
  if not (UTILITY_FT[vim.bo[buf].filetype] or UTILITY_BT[vim.bo[buf].buftype]) then
    if cur ~= "" and vim.fn.isdirectory(vim.fn.expand(cur)) == 1 then
      return vim.fn.fnamemodify(cur, ":p")
    end
  end
  return vim.fn.getcwd()
end

---Nearest ancestor containing go.mod (optional module-aware scope).
function M.go_module_dir()
  local cur = M.source_dir()
  -- keep walking up from the source dir itself
  if vim.loop.fs_stat(cur .. "/go.mod") then return cur end
  local parent = vim.fn.fnamemodify(cur, ":h")
  while parent and parent ~= cur and parent ~= "/" do
    if vim.loop.fs_stat(parent .. "/go.mod") then return parent end
    local next_parent = vim.fn.fnamemodify(parent, ":h")
    if next_parent == parent then break end
    parent = next_parent
  end
  return nil
end

---Worktree root: nearest ancestor with a .git entry — directory (main
---checkout) OR FILE (linked worktree) — then go.mod, then cwd. vim.fs.root
---matches markers of either type, so linked worktrees resolve correctly.
function M.worktree_root()
  local root = vim.fs.root(0, ".git")
  if root then return root end
  root = vim.fs.root(0, "go.mod")
  if root then return root end
  return vim.fn.getcwd()
end

return M