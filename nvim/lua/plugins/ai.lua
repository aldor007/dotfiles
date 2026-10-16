-- Inline AI completion (minuet-ai): suggestions as you type, blink.cmp
-- frontend + virtualtext. Complements codecompanion chat (not installed yet).
-- Needs an API key in env (ANTHROPIC_API_KEY / OPENAI_API_KEY / GEMINI_API_KEY)
-- or an Ollama endpoint; minuet is skipped automatically until one is set.
return {
  {
    "milanglacier/minuet-ai.nvim",
    event = "InsertEnter",
    dependencies = { "saghen/blink.cmp" },
    -- blink provider + auto-complete wiring lives in lsp.lua
    -- (sources.providers.minuet); blink.enable_auto_complete defaults true.
    keys = {
      { "<A-y>", function() require("minuet").make_blink_map()() end, desc = "ai: minuet invoke", mode = "i" },
    },
    opts = {},
  },
}