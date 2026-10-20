-- trouble: one list for LSP diagnostics (workspace/buffer) + document
-- symbols — the "investigate diagnostics" workbench. Lazy-loaded.
return {
  {
    "folke/trouble.nvim",
    cmd = { "Trouble" },
    keys = {
      { "<leader>xx", "<cmd>Trouble diagnostics toggle<cr>", desc = "diagnostics: workspace" },
      { "<leader>xX", "<cmd>Trouble diagnostics toggle filter.buf=0<cr>", desc = "diagnostics: buffer" },
      { "<leader>xs", "<cmd>Trouble symbols toggle focus=false<cr>", desc = "symbols" },
    },
    opts = {},
  },
}