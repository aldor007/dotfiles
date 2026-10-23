-- Debugging: nvim-dap (core) + dap-ui (panes) + mason-nvim-dap (adapters).
-- Adapters installed via mason: delve (Go), debugpy (Python); add "chrome"
-- for TS/JS if needed. Keymaps use the `,d*` namespace (leader is `,`).
return {
  {
    "mfussenegger/nvim-dap",
    lazy = true,
    -- mason-nvim-dap is intentionally NOT a dependency: nvim-dap is the core
    -- debugger; the bridge plugin (mason-nvim-dap) sits on top and must
    -- depend on nvim-dap, never the other way around — audit finding P1.
    dependencies = {},
    keys = {
      { "<leader>db", function() require("dap").toggle_breakpoint() end, desc = "dap: toggle breakpoint" },
      { "<leader>dc", function() require("dap").continue() end, desc = "dap: continue/start" },
      { "<leader>do", function() require("dap").step_over() end, desc = "dap: step over" },
      { "<leader>di", function() require("dap").step_into() end, desc = "dap: step into" },
      { "<leader>du", function() require("dap").step_out() end, desc = "dap: step out" },
      { "<leader>dut", function() require("dap").terminate() end, desc = "dap: terminate" },
    },
  },

  {
    "jay-babu/mason-nvim-dap.nvim",
    lazy = true,
    event = "VeryLazy", -- audit P1: register adapters + handlers after UiEnter,
                          -- so they exist before any Dap* keypress fires.
    dependencies = { "mason.nvim", "mfussenegger/nvim-dap" },
    opts = {
      -- ensure_installed uses mason-nvim-dap's adapter IDs, not pip
      -- distribution names: 'python' (not 'debugpy' — the adapter
      -- registers itself under 'python' regardless of the underlying
      -- library). audit finding P1.
      ensure_installed = { "delve", "python" },
      automatic_setup = true,
      handlers = {},
    },
  },

  {
    "rcarriga/nvim-dap-ui",
    lazy = true,
    event = "VeryLazy", -- audit P2: register auto-open / auto-close listeners
                          -- early so ,dc with no ,dui still wires them up.
    dependencies = { "mfussenegger/nvim-dap", "nvim-neotest/nvim-nio" },
    keys = {
      { "<leader>dui", function() require("dapui").toggle() end, desc = "dap: toggle ui" },
    },
    config = function()
      local dap, dapui = require("dap"), require("dapui")
      dapui.setup({})
      dap.listeners.after.event_initialized["dapui_config"] = function() dapui.open() end
      dap.listeners.before.event_terminated["dapui_config"] = function() dapui.close() end
      dap.listeners.before.event_exited["dapui_config"] = function() dapui.close() end
    end,
  },
}