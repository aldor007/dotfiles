-- aerial: symbol outline for navigating large Go/k8s repos (treesitter
-- backend; falls back to LSP). Lazy-loaded on first keypress.
-- NOTE: ,a* is taken by herdr-nvim annotations — the outline lives on ,o.
return {
  {
    "stevearc/aerial.nvim",
    -- both key-based (',o') AND command-based loading: typing :AerialToggle
    -- must work before the key is ever pressed (lazy needs cmd triggers for
    -- commands to auto-load the plugin)
    cmd = { "AerialToggle", "AerialNavToggle" },
    keys = {
      { "<leader>o", "<cmd>AerialToggle!<cr>", desc = "code: symbol outline" },
      { "<leader>on", "<cmd>AerialNavToggle<cr>", desc = "code: outline nav" },
    },
    opts = {}, -- backends default: treesitter preferred, LSP fallback
  },
}