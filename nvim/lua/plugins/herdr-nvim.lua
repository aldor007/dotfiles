-- herdr-nvim: nvim half of the herdr sidebar (herdr side installed via
-- install.sh; bindings in .config/herdr/config.toml: prefix+e toggle,
-- prefix+f pick-file). This half provides code annotations sent to the agent.
--
-- Keymaps are bound HERE against the plugin's Lua API (keymaps = false):
-- the plugin's built-in binder skips ("not overriding existing map") when it
-- thinks a key is taken, so the config decides. Same keys as upstream:
--   ,ac comment line/selection   ,al list comments   ,as paste   ,aS submit
--   ,ai reference line/selection at the agent cursor
-- :Herdr (tab-completes) is the command equivalent.
return {
  {
    "ChmaraX/herdr-nvim",
    event = "VeryLazy", -- no startup cost until idle-after-start
    opts = { keymaps = false },
    config = function(_, opts)
      local h = require("herdr-nvim")
      h.setup(opts)
      local map = vim.keymap.set
      map("n", "<leader>ac", function() h.comment_line() end, { desc = "herdr: comment line" })
      map("x", "<leader>ac", function() h.comment_selection() end, { desc = "herdr: comment selection" })
      map("n", "<leader>al", function() h.list_comments() end, { desc = "herdr: list comments" })
      map("n", "<leader>as", function() h.send_all({ submit = false }) end, { desc = "herdr: paste comments to agent" })
      map("n", "<leader>aS", function() h.send_all({ submit = true }) end, { desc = "herdr: send comments to agent" })
      map("n", "<leader>ai", function() h.ref_line() end, { desc = "herdr: reference line at agent cursor" })
      map("x", "<leader>ai", function() h.ref_selection() end, { desc = "herdr: reference selection at agent cursor" })
    end,
  },
}