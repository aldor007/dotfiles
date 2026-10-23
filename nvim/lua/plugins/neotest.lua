-- neotest: run Go tests from the editor (nearest / file / output / stop).
-- neotest-golang runs plain `go test` in the package dir (no gotestsum
-- dependency). Treesitter must have the go parser (it does: treesitter.lua).
return {
  {
    "nvim-neotest/neotest",
    dependencies = {
      "nvim-neotest/nvim-nio",
      "nvim-lua/plenary.nvim",
      "antoinemadec/FixCursorHold.nvim", -- required upstream
      "fredrikaverpil/neotest-golang",
    },
    keys = {
      { "<leader>tt", function() require("neotest").run.run() end, desc = "test: nearest" },
      { "<leader>tf", function() require("neotest").run.run(vim.fn.expand("%")) end, desc = "test: file" },
      { "<leader>to", function() require("neotest").output.open({ enter = true, auto_close = true }) end, desc = "test: output" },
      { "<leader>ts", function() require("neotest").summary.toggle() end, desc = "test: summary" },
      { "<leader>tx", function() require("neotest").run.stop() end, desc = "test: stop" },
    },
    opts = {
      -- Monorepo tuning (per neotest-golang troubleshooting docs): project-wide
      -- AST discovery is what lags in huge repos — parse only the opened buffer.
      discovery = {
        enabled = false,
        concurrent = 1,
      },
      running = { concurrent = true },
      summary = { animated = false },
    },
    -- the adapter require stays OUT of the spec table: it must not run at
    -- spec-load time (Lazy! clean removes unpackaged plugins and the cache
    -- loader then fails the whole config load)
    config = function(_, opts)
      opts.adapters = {
        require("neotest-golang")({
          runner = "go", -- plain `go test`; env inherits from the shell
          filter_dir_patterns = { -- skip trees that never hold Go tests
            "vendor", "testdata", "node_modules", ".git", "docs",
          },
        }),
      }
      require("neotest").setup(opts)
    end,
  },
}
