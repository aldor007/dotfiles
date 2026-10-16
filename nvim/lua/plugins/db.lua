-- Database access: vim-dadbod (engine) + dadbod-ui (connection browser)
-- + dadbod-completion (schema-aware completion, blink.cmp source).
--   :DBUIToggle      — connection browser sidebar
--   :DB              — ad-hoc query on the current connection
--   :DBUIAddConnection — add a connection (host/user/db or URI)
-- Completion in .sql buffers is wired into blink.cmp in lsp.lua
-- (sources.per_filetype.sql + providers.dadbod).
return {
  {
    "tpope/vim-dadbod",
    cmd = { "DB", "DBUIToggle", "DBUIAddConnection", "DBUIFindBuffer" },
    keys = {
      { "<leader>dbx", function() require("dbui").toggle() end, desc = "db: toggle browser" },
    },
  },
  {
    "kristijanhusak/vim-dadbod-ui",
    dependencies = { { "tpope/vim-dadbod", lazy = true } },
    cmd = { "DBUI", "DBUIToggle", "DBUIAddConnection", "DBUIFindBuffer" },
    init = function()
      vim.g.db_ui_use_nerd_fonts = 1
      vim.g.db_ui_show_help = 1
    end,
  },
  {
    "kristijanhusak/vim-dadbod-completion",
    ft = { "sql", "mysql", "plsql" },
    dependencies = { "tpope/vim-dadbod" },
  },
}