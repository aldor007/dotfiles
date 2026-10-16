-- nvim-tree: file explorer (replaces mini.files, classic NERDTree-style tree)
return {
  {
    "nvim-tree/nvim-tree.lua",
    cmd = { "NvimTreeToggle", "NvimTreeFindFile", "NvimTreeOpen" },
    keys = {
      { "<C-n>", "<cmd>NvimTreeToggle<cr>", desc = "Toggle file tree" },
      { "<leader>e", "<cmd>NvimTreeFindFile<cr>", desc = "Reveal file in tree" },
      { "<leader>nt", "<cmd>NvimTreeFindFile<cr>", desc = "Reveal file in tree" },
    },
    dependencies = { "nvim-tree/nvim-web-devicons" },
    opts = {
      -- NERDTree-style 't' = open in a new tab (keeps all default mappings)
      on_attach = function(bufnr)
        local api = require("nvim-tree.api")
        local function opts(desc)
          return { desc = "nvim-tree: " .. desc, buffer = bufnr, noremap = true, silent = true, nowait = true }
        end
        api.map.on_attach.default(bufnr)
        vim.keymap.set("n", "t", api.node.open.tab, opts("Open: New Tab"))
      end,
      -- Perf: disable filesystem + git watchers & per-project git status (major
      -- lag source in large repos). Signs/icons per file are dropped.
      git = { enable = false },
      filesystem_watchers = { enable = false },
      sync_root_with_cwd = true,
      filters = {
        dotfiles = false,
        custom = { "\\.pyc$", "\\.swo$", "\\.swp$" },
      },
      view = { side = "left", width = 32 },
      renderer = { group_empty = true },
      update_focused_file = { enable = true },
    },
  },
}
