-- Hardtime: gentle guidance for better motions without blocking normal review.
-- Use :Hardtime toggle to suspend/restart, :Hardtime report to inspect patterns.
return {
  {
    "m4xshen/hardtime.nvim",
    lazy = false,
    dependencies = { "MunifTanjim/nui.nvim" },
    config = function(_, opts)
      -- hardtime appends its log to XDG_STATE_HOME/nvim/ at load time. On a
      -- fresh machine/CI that directory doesn't exist, the open fails, and
      -- hardtime silently disables itself — so create it before setup.
      vim.fn.mkdir(vim.fn.stdpath("state") .. "/nvim", "p")
      require("hardtime").setup(opts)
    end,
    keys = {
      { "<leader>uh", "<cmd>Hardtime toggle<cr>", desc = "learning: toggle Hardtime" },
      { "<leader>ur", "<cmd>Hardtime report<cr>", desc = "learning: open Hardtime report" },
    },
    opts = {
      enabled = true,
      restriction_mode = "block", -- actually restrict (hint = true still tells you what to press)
      hint = true,
      notification = true,
      disable_mouse = false,
      max_count = 3,
      max_time = 1000,
      disabled_filetypes = {
        "lazy",
        "neo-tree",
        "DiffviewFiles",
        "Outline",
        "TelescopePrompt",
        "notify",
        "qf",
        "help",
      },
    },
  },
}
