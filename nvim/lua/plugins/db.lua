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
      -- ,dk / ,dD / ,dx: connect via scripts/sql-db.sh (px IAP tunnel +
      -- Cloud SQL IAM; see scripts/sql-db.sh --help). Sets g:db + console.
      { "<leader>dk", function() require("config.db").use_env("prod-master") end, desc = "db: connect prod-master (service-a)" },
      {
        "<leader>dD",
        function()
          vim.ui.select(
            { "dev-master", "prod-master", "prod-eu", "prod-india" },
            { prompt = "Connect DB env" },
            function(env)
              if env then require("config.db").use_env(env) end
            end
          )
        end,
        desc = "db: connect env (picker)",
      },
      { "<leader>dx", function() require("config.db").stop() end, desc = "db: stop tunnel" },
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