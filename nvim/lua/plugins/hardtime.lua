-- Hardtime: breaks bad habits (repeated h/j/k/l, arrow keys) by hinting
-- better motions. :Hardtime report | :Hardtime toggle to pause/resume.
-- Defaults: max_count=3 repeats per 1s; disable_mouse=true.
return {
  {
    "m4xshen/hardtime.nvim",
    lazy = false,
    dependencies = { "MunifTanjim/nui.nvim" },
    opts = {
      -- keymaps.lua maps j/k -> gj/gk (wrapped-line scrolling, ported from .vimrc).
      -- Hardtime restricts j/k/gj/gk by default, which would silently kill that
      -- motion. Disable those four; h/l/arrows/mouse restrictions stay active.
      restricted_keys = {
        ["j"] = false,
        ["k"] = false,
        ["gj"] = false,
        ["gk"] = false,
      },
    },
  },
}
