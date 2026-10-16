-- Hardtime: breaks bad habits (repeated h/j/k/l, arrow keys) by hinting
-- better motions. :Hardtime report | :Hardtime toggle to pause/resume.
-- Defaults: max_count=3 repeats per 1s; disable_mouse=true.
return {
  {
    "m4xshen/hardtime.nvim",
    lazy = false,
    dependencies = { "MunifTanjim/nui.nvim" },
    opts = {},
  },
}
