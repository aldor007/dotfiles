-- aerial: symbol outline for navigating large Go/k8s repos (treesitter
-- backend; falls back to LSP). Lazy-loaded on first keypress.
-- NOTE: ,a* is taken by herdr-nvim annotations — the outline lives on ,o.
return {
  {
    "stevearc/aerial.nvim",
    keys = {
      { "<leader>o", "<cmd>AerialToggle!<cr>", desc = "code: symbol outline" },
      { "<leader>on", "<cmd>AerialNavToggle<cr>", desc = "code: outline nav" },
    },
    opts = {}, -- backends default: treesitter preferred, LSP fallback
  },
}