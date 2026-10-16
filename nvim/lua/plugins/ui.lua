-- UI: colorscheme, statusline, key discoverability
return {
  -- Colorscheme (swap this one line to change theme)
  {
    "rebelot/kanagawa.nvim",
    lazy = false,
    priority = 1000,
    opts = {},
    config = function(_, opts)
      require("kanagawa").setup(opts)
      -- variants: kanagawa (wave, default) | kanagawa-dragon (darker) | kanagawa-lotus (light)
      vim.cmd.colorscheme("kanagawa-dragon")
    end,
  },

  -- Statusline (replaces vim-airline)
  {
    "nvim-lualine/lualine.nvim",
    event = "VeryLazy",
    dependencies = { "nvim-tree/nvim-web-devicons" },
    opts = {
      options = {
        theme = "auto",
        component_separators = "|",
        section_separators = "",
      },
      sections = {
        lualine_c = { { "filename", path = 1 } },
      },
    },
  },

  -- Which-key: discoverable keymaps, shows leader hints after pause
  {
    "folke/which-key.nvim",
    event = "VeryLazy",
    opts = {
      spec = {
        { "<leader>f", group = "fold level" },
        { "<leader>c", group = "code (LSP)" },
      },
    },
  },

  -- Indent guides
  {
    "lukas-reineke/indent-blankline.nvim",
    event = "BufReadPre",
    main = "ibl",
    opts = {},
  },
}
