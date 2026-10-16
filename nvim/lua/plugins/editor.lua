-- Core editing: mini.nvim modules + git (replaces vim-commentary, vim-surround,
-- delimitMate, tabular, gitgutter, gundo)
return {
  -- Git wrapper (still maintained, canonical)
  { "tpope/vim-fugitive", cmd = { "Git", "G", "Gdiffsplit", "Gblame" } },

  -- Git history/tree browser: commit history, per-file history, diff views
  -- (keymaps: <leader>go/gl/gH/gc defined in lua/config/keymaps.lua)
  {
    "sindrets/diffview.nvim",
    cmd = { "DiffviewOpen", "DiffviewFileHistory", "DiffviewClose", "DiffviewToggleFiles" },
    keys = {
      { "<leader>go", "<cmd>DiffviewOpen<cr>", desc = "git: working tree diff" },
      { "<leader>gl", "<cmd>DiffviewFileHistory %<cr>", desc = "git: file history" },
      { "<leader>gH", "<cmd>DiffviewFileHistory<cr>", desc = "git: repo history" },
      { "<leader>gc", "<cmd>DiffviewClose<cr>", desc = "git: close diff view" },
    },
    opts = {
      view = {
        merge_tool = { layout = "diff3_mixed" },
      },
    },
  },

  -- Git gutter signs + hunk staging (replaces vim-gitgutter)
  {
    "lewis6991/gitsigns.nvim",
    event = "BufReadPre",
    opts = {
      signs = {
        add = { text = "+" },
        change = { text = "~" },
        delete = { text = "_" },
        topdelete = { text = "‾" },
        changedelete = { text = "~" },
      },
    },
  },

  -- Auto-close brackets (replaces delimitMate)
  { "echasnovski/mini.pairs", version = "*", event = "InsertEnter", opts = {} },

  -- Surround (replaces vim-surround): sa/sd/sr + variants
  {
    "echasnovski/mini.surround",
    version = "*",
    event = "BufReadPre",
    opts = {
      mappings = {
        add = "sa",
        delete = "sd",
        find = "sf",
        find_left = "sF",
        highlight = "sh",
        replace = "sr",
        update_n_lines = "sn",
      },
    },
  },

  -- Comments (replaces vim-commentary): gc/gbc + gcc/gbc line ops
  {
    "echasnovski/mini.comment",
    version = "*",
    event = "BufReadPre",
    opts = {},
  },

  -- Text alignment (replaces tabular): ga interactive, gA preview
  {
    "echasnovski/mini.align",
    version = "*",
    event = "BufReadPre",
    opts = {},
  },

  -- Undo history visualizer (replaces Gundo)
  { "mbbill/undotree", cmd = "UndotreeToggle" },
}
