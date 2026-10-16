-- Formatting + linting (was a gap: only LSP diagnostics existed).
-- conform: format on save with LSP fallback; per-filetype formatters.
--   Binaries needed (missing ones are skipped gracefully by conform):
--   gofmt (ships with Go), stylua, ruff (python), prettier (ts/js),
--   terraform fmt (tfm binary is `terraform`).
-- nvim-lint: linters that are NOT language servers (ruff python linting,
--   shellcheck). LSP servers (gopls/ts_ls/basedpyright) already cover the rest.
return {
  {
    "stevearc/conform.nvim",
    event = "BufWritePre",
    cmd = "ConformInfo",
    keys = {
      {
        "<leader>cf",
        function() require("conform").format({ async = true, lsp_fallback = true }) end,
        desc = "format: buffer",
      },
    },
    opts = {
      format_on_save = { timeout_ms = 500, lsp_fallback = true },
      formatters_by_ft = {
        go = { "gofmt" },
        lua = { "stylua" },
        python = { "ruff_format" },
        terraform = { "terraform_fmt" },
        hcl = { "terraform_fmt" },
        javascript = { "prettierd", "prettier", stop_after_first = true },
        typescript = { "prettierd", "prettier", stop_after_first = true },
        json = { "prettierd", "prettier", stop_after_first = true },
        yaml = { "prettierd", "prettier", stop_after_first = true },
      },
    },
  },

  {
    "mfussenegger/nvim-lint",
    main = "lint", -- the top-level module is lua/lint.lua
    event = { "BufReadPre", "BufNewFile" },
    config = function()
      require("lint").linters_by_ft = {
        python = { "ruff" },
        sh = { "shellcheck" },
        bash = { "shellcheck" },
      }
      vim.api.nvim_create_autocmd({ "BufWritePost", "InsertLeave" }, {
        callback = function() require("lint").try_lint() end,
      })
    end,
  },
}