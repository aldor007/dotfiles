-- Tabline: barbar.nvim — tabs as understood by any other editor (per-buffer bar
-- with icons, pin/reorder, jump-to mode). Docs: romgrk/barbar.nvim (README).
-- Optional deps declared per README — both already installed (devicons via
-- nvim-tree/lualine, gitsigns via editor.lua keymaps).
--
-- Keymap choice (divergence from the README, deliberate):
--   README maps BufferPick to <C-p> — we DON'T: <C-p>/<C-b> stay Telescope.
--   BufferPick lives on <space>bp instead.
-- Alt mappings need the terminal's Option key mapped to "Esc+" (iTerm:
-- Profiles > Keys > Left Option key = Esc+), else use the <space>b* fallback.
--
-- tabman.nvim is bundled here too (same domain): interactive TABPAGE manager
-- (:Tabman) — complements barbar, which manages BUFFER bar only. No overlap.
return {
  {
    "romgrk/barbar.nvim",
    -- v1.9.1 tag (2024-07) is the newest RELEASE but master carries 20+ unreleased
    -- fixes (tab title sizing, gitsigns/remote buffers, hl reset on &bg change...).
    -- Follow master; `:Lazy sync` to update.
    branch = "master",
    event = { "BufReadPre", "BufNewFile" },
    dependencies = {
      "lewis6991/gitsigns.nvim", -- OPTIONAL per README: git status in tabs
      "echasnovski/mini.icons", -- icons (devicons replaced; mock keeps compat)
    },
    init = function()
      vim.g.barbar_auto_setup = false
    end,
    config = function(_, opts)
      require("barbar").setup(opts)
      -- Barbar always renders a full-width tabline; with few buffers the
      -- remainder is spaces in BufferTabpageFill -> TabLineFill, whose bg
      -- (#181616) equals the editor bg, so the bar LOOKS like it stops ~2/3
      -- across. Give the fill a distinct panel shade (kanagawa dragonBlack4).
      vim.api.nvim_set_hl(0, "BufferTabpageFill", { bg = "#282727" })
    end,
    opts = {
      -- Sidebar offsets so nvim-tree/diffview don't cover the buffer bar
      sidebar_filetypes = {
        NvimTree = true,
        DiffviewFiles = true,
      },
      icons = {
        diagnostics = {
          [vim.diagnostic.severity.ERROR] = { enabled = true },
          [vim.diagnostic.severity.WARN] = { enabled = true },
          [vim.diagnostic.severity.INFO] = { enabled = false },
          [vim.diagnostic.severity.HINT] = { enabled = false },
        },
        gitsigns = {
          added = { enabled = true, icon = "+" },
          changed = { enabled = true, icon = "~" },
          deleted = { enabled = true, icon = "-" },
        },
      },
    },
    keys = {
      -- Move through buffers (README canonical)
      { "<A-,>", "<cmd>BufferPrevious<cr>", desc = "bar: prev buffer" },
      { "<A-.>", "<cmd>BufferNext<cr>", desc = "bar: next buffer" },
      -- Re-order buffers
      { "<A-<>", "<cmd>BufferMovePrevious<cr>", desc = "bar: move buffer left" },
      { "<A->>", "<cmd>BufferMoveNext<cr>", desc = "bar: move buffer right" },
      -- Goto buffer by position / last
      { "<A-1>", "<cmd>BufferGoto 1<cr>", desc = "bar: goto buffer 1" },
      { "<A-2>", "<cmd>BufferGoto 2<cr>", desc = "bar: goto buffer 2" },
      { "<A-3>", "<cmd>BufferGoto 3<cr>", desc = "bar: goto buffer 3" },
      { "<A-4>", "<cmd>BufferGoto 4<cr>", desc = "bar: goto buffer 4" },
      { "<A-5>", "<cmd>BufferGoto 5<cr>", desc = "bar: goto buffer 5" },
      { "<A-6>", "<cmd>BufferGoto 6<cr>", desc = "bar: goto buffer 6" },
      { "<A-7>", "<cmd>BufferGoto 7<cr>", desc = "bar: goto buffer 7" },
      { "<A-8>", "<cmd>BufferGoto 8<cr>", desc = "bar: goto buffer 8" },
      { "<A-9>", "<cmd>BufferGoto 9<cr>", desc = "bar: goto buffer 9" },
      { "<A-0>", "<cmd>BufferLast<cr>", desc = "bar: last buffer" },
      -- Pin / close / restore (close uses bbye semantics: layout is preserved)
      { "<A-p>", "<cmd>BufferPin<cr>", desc = "bar: pin/unpin buffer" },
      { "<A-c>", "<cmd>BufferClose<cr>", desc = "bar: close buffer (keep layout)" },
      { "<A-S-c>", "<cmd>BufferRestore<cr>", desc = "bar: restore closed buffer" },
      -- Magic buffer-picking mode + order-by (README <Space>b* namespace)
      { "<space>bp", "<cmd>BufferPick<cr>", desc = "bar: pick buffer (jump mode)" },
      { "<space>bP", "<cmd>BufferPickDelete<cr>", desc = "bar: pick buffer to delete" },
      { "<space>bb", "<cmd>BufferOrderByBufferNumber<cr>", desc = "bar: order by number" },
      { "<space>bn", "<cmd>BufferOrderByName<cr>", desc = "bar: order by name" },
      { "<space>bd", "<cmd>BufferOrderByDirectory<cr>", desc = "bar: order by directory" },
      { "<space>bl", "<cmd>BufferOrderByLanguage<cr>", desc = "bar: order by language" },
      { "<space>bw", "<cmd>BufferOrderByWindowNumber<cr>", desc = "bar: order by window" },
    },
  },

  -- Tabpage/window manager popup (:Tabman). Keys inside the popup: q close,
  -- o toggle-tabpage-expand, x delete-tabpage, <Enter> jump to window.
  {
    "wsdjeg/tabman.nvim",
    cmd = { "Tabman" },
    keys = {
      { "<leader>t", "<cmd>Tabman<cr>", desc = "tabs: manager (:Tabman)" },
    },
  },
}
