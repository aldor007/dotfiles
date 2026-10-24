-- Hardtime: gentle guidance for better motions without blocking normal review.
-- Use :Hardtime toggle to suspend/restart, :Hardtime report to inspect patterns.
return {
  {
    "m4xshen/hardtime.nvim",
    lazy = false,
    dependencies = { "MunifTanjim/nui.nvim" },
    keys = {
      { "<leader>uh", "<cmd>Hardtime toggle<cr>", desc = "learning: toggle Hardtime" },
      { "<leader>ur", "<cmd>Hardtime report<cr>", desc = "learning: open Hardtime report" },
    },
    opts = {
      enabled = true,
      restriction_mode = "hint",
      hint = true,
      notification = false,
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
