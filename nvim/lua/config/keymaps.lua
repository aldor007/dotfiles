-- Keymaps ported from the old .vimrc
local map = vim.keymap.set

-- Leader: ',' (same as old config) — must be set before lazy.nvim loads
vim.g.mapleader = ","
vim.g.maplocalleader = ","

-- ';' behaves like ':' (and fix lazy-shift typos)
map("n", ";", ":")
map("c", "WQ", "wq")
map("c", "wQ", "wq")
map("c", "Tabe", "tabe")

-- Window navigation + maximize: <C-J/K/L/H>
map("n", "<C-J>", "<C-W>j<C-W>_")
map("n", "<C-K>", "<C-W>k<C-W>_")
map("n", "<C-L>", "<C-W>l<C-W>_")
map("n", "<C-H>", "<C-W>h<C-W>_")

-- Wrapped lines: j/k move visually
map("n", "j", "gj")
map("n", "k", "gk")

-- Tabs/buffers with shift: <S-H> previous, <S-L> next.
-- NOTE: nvim coalesces <S-H> == H (terminals deliver 'H'), so this also
-- replaces the default H='top of screen' / L. Switches the barbar buffer
-- bar (gt/gT stay available for real tabpages).
map("n", "<S-H>", ":BufferPrevious<CR>")
map("n", "<S-L>", ":BufferNext<CR>")

-- Y yanks to end of line (consistent with C/D)
map("n", "Y", "y$")

-- Visual indent keeps selection
map("v", "<", "<gv")
map("v", ">", ">gv")

-- Clear search highlight: ,/
map("n", "<leader>/", ":nohlsearch<CR>")

-- lcd to current file dir: :cwd / :cd.
map("c", "cwd", "lcd %:p:h")
map("c", "cd.", "lcd %:p:h")

-- sudo write: :w!!
map("c", "w!!", "w !sudo tee % >/dev/null")

-- Fold levels: ,f0..,f9
for i = 0, 9 do
  map("n", "<leader>f" .. i, ":set foldlevel=" .. i .. "<CR>")
end

-- Git: blame + hunks via gitsigns (no-ops when buffer isn't git-attached)
local function gs(fn)
  return function()
    require("gitsigns")[fn]()
  end
end
map("n", "<leader>gb", gs("blame_line"), { desc = "git: blame line popup" })
map("n", "<leader>gB", ":Gitsigns blame<CR>", { desc = "git: full blame window" })
map("n", "<leader>gt", gs("toggle_current_line_blame"), { desc = "git: toggle inline blame" })
map("n", "]h", gs("next_hunk"), { desc = "git: next hunk" })
map("n", "[h", gs("prev_hunk"), { desc = "git: previous hunk" })
map("n", "<leader>hs", gs("stage_hunk"), { desc = "git: stage hunk" })
map("n", "<leader>hr", gs("reset_hunk"), { desc = "git: reset hunk" })
map("n", "<leader>hp", gs("preview_hunk"), { desc = "git: preview hunk" })

-- JSON pretty print: ,jt
map("n", "<leader>jt", ":%!python3 -m json.tool<CR>:set filetype=json<CR>")

-- List every available keymap across all modes: ,kk
-- Opens a scratch window with every mapping defined in the config (and any
-- active plugin maps), grouped by mode. Independent of Telescope/which-key.
local function list_all_keymaps()
  local modes = {
    { "Normal", "n" },
    { "Visual", "v" },
    { "Select", "s" },
    { "Operator-pending", "o" },
    { "Insert", "i" },
    { "Command-line", "c" },
    { "Terminal", "t" },
  }

  local lines = { "== All keymaps ==" }
  for _, md in ipairs(modes) do
    local maps = vim.api.nvim_get_keymap(md[2])
    if #maps > 0 then
      table.insert(lines, "")
      table.insert(lines, "-- " .. md[1] .. " (mode " .. md[2] .. ") --")
      for _, m in ipairs(maps) do
        local rhs = m.rhs and m.rhs ~= "" and m.rhs
          or (m.callback and "<Lua callback>")
          or ""
        if m.desc and m.desc ~= "" then
          rhs = rhs .. "    " .. m.desc
        end
        table.insert(lines, string.format("%-22s %s", m.lhs, rhs))
      end
    end
  end

  vim.cmd("new") -- fresh scratch window
  local buf = vim.api.nvim_get_current_buf()
  vim.bo[buf].buftype = "nofile"
  vim.bo[buf].bufhidden = "wipe"
  vim.bo[buf].modifiable = true
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false
  vim.api.nvim_win_set_cursor(0, { 1, 0 })
end

vim.api.nvim_create_user_command("ListKeymaps", list_all_keymaps, {})
map("n", "<leader>kk", "<cmd>ListKeymaps<cr>", { desc = "List all keymaps" })

-- Return to last edit position on open (from old vimrc)
vim.api.nvim_create_autocmd("BufReadPost", {
  pattern = "*",
  callback = function()
    local pos = vim.api.nvim_buf_get_mark(0, '"')
    if pos[1] > 0 and pos[1] <= vim.api.nvim_buf_line_count(0) then
      pcall(vim.api.nvim_win_set_cursor, 0, pos)
    end
  end,
})
