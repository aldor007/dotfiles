-- Inline AI completion (minuet-ai): suggestions as you type, blink.cmp
-- frontend + virtualtext. Complements codecompanion chat (not installed yet).
-- Needs an API key in env (ANTHROPIC_API_KEY / OPENAI_API_KEY / GEMINI_API_KEY)
-- or an Ollama endpoint; minuet is skipped automatically until one is set.
-- Kimchi gateway key: prefer the env (exported in .zshrc for new shells),
-- fall back to reading kimchi's config.json directly — so nvim sessions
-- started before the .zshrc change (or from non-interactive shells) work too.
--
-- blink.cmp wiring: the after/ shim at
-- nvim/after/blink/completion/source/minuet.lua bridges blink.cmp's
-- `require('blink.completion.source.minuet')` lookup to
-- `require('minuet.blink')`. minuet's own init.lua only registers with
-- nvim-cmp (`if has_cmp then ...`), so blink.cmp users get nothing
-- without the shim — the source is in `sources.default` (lsp.lua:128)
-- but never resolves. See the shim file for the rationale.
local function kimchi_api_key()
  local v = vim.env.KIMCHI_API_KEY
  if v and v ~= "" then return v end
  local path = vim.fn.expand("~/.config/kimchi/config.json")
  if vim.fn.filereadable(path) == 1 then
    local ok, data = pcall(vim.fn.json_decode, vim.fn.readfile(path))
    if
      ok
      and type(data) == "table"
      and type(data.apiKey) == "string"
      and data.apiKey ~= ""
    then
      return data.apiKey
    end
  end
  return nil
end

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
    opts = {
      -- Use the Kimchi LLM gateway (llm.kimchi.dev/openai/v1 — OpenAI-compatible,
      -- authenticated with the user's existing kimchi API key from
      -- ~/.config/kimchi/config.json, exported as KIMCHI_API_KEY in .zshrc).
      -- Model choice: deepseek-v4-flash-0731 answers instantly; glm-5.3-flash /
      -- kimi-k2.7 burn the token budget on reasoning before emitting code.
      provider = "openai_compatible",
      provider_options = {
        openai_compatible = {
          name = "Kimchi",
          end_point = "https://llm.kimchi.dev/openai/v1/chat/completions",
          api_key = kimchi_api_key, -- function form: env, else kimchi config.json
          model = "deepseek-v4-flash-0731",
          optional = { max_tokens = 256 },
        },
      },
      -- Gate auto-completion until the gateway key is present (otherwise the
      -- provider fires a red ERROR notify on every completion request).
      enable_predicates = {
        function()
          if kimchi_api_key() then return true end
          for _, key in ipairs({
            "CODESTRAL_API_KEY", "MISTRAL_API_KEY", "ANTHROPIC_API_KEY",
            "OPENAI_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY", "GROQ_API_KEY",
          }) do
            -- local: without it this `v` leaked into the enclosing module
            -- scope (visible to any later code in this spec). Local keeps
            -- the iteration scope.
            local v = vim.env[key]
            if v and v ~= "" then return true end
          end
          return false
        end,
      },
    },
  },
}