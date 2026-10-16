-- Flash: label-jump motions (jump to any visible word/char/treesitter node).
-- Complements hardtime — its hints push you to f/t/w motions; flash makes
-- them single-key. Conflicts to know: bare `s` now starts flash instead of
-- Vim's substitute; mini.surround's two-key `sa/sd/sr/...` still win.
return {
  {
    "folke/flash.nvim",
    event = "VeryLazy",
    opts = {},
    keys = {
      { "s", mode = { "n", "x", "o" }, function() require("flash").jump() end, desc = "flash: jump to char" },
      { "S", mode = { "n", "x", "o" }, function() require("flash").treesitter() end, desc = "flash: treesitter node" },
      { "r", mode = "o", function() require("flash").remote() end, desc = "flash: remote (operator)" },
      { "R", mode = { "o", "x" }, function() require("flash").treesitter_search() end, desc = "flash: treesitter search" },
      { "<c-s>", mode = { "c" }, function() require("flash").toggle() end, desc = "flash: toggle search highlight" },
    },
  },
}