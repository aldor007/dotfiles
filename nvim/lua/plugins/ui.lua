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

  -- nui.nvim: UI component library (Popup/Layout/Input/Menu/Split + text
  -- blocks). Zero-deps, no setup() — modules are required directly by other
  -- plugins (lazy-loaded library).
  {
    "MunifTanjim/nui.nvim",
    lazy = true,
  },

  -- nvim-notify: real floating notifications sink for every plugin's vim.notify
  -- (hardtime hints, mason, LSP, …). Must own vim.notify as early as possible,
  -- so NOT lazy. termguicolors already enabled (options.lua).
  {
    "rcarriga/nvim-notify",
    lazy = false,
    priority = 800,
    opts = {
      stages = "fade_in_slide_out",
      timeout = 3000,
      render = "default",
    },
    config = function(_, opts)
      require("notify").setup(opts)
      vim.notify = require("notify")
    end,
  },
}
