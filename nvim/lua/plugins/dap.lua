-- Debugging: nvim-dap (core) + dap-ui (panes) + mason-nvim-dap (adapters).
-- Adapters installed via mason: delve (Go), debugpy (Python); add "chrome"
-- for TS/JS if needed. Keymaps use the `,d*` namespace (leader is `,`).
return {
  {
    "mfussenegger/nvim-dap",
    lazy = true,
    dependencies = { "jay-babu/mason-nvim-dap.nvim" },
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
    dependencies = { "mason.nvim", "mfussenegger/nvim-dap" },
    opts = {
      ensure_installed = { "delve", "debugpy" },
      automatic_setup = true,
      handlers = {},
    },
  },

  {
    "rcarriga/nvim-dap-ui",
    lazy = true,
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