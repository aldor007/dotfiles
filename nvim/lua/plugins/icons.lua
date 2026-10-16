-- mini.icons replaces nvim-web-devicons (same author as the rest of the
-- mini suite: faster, fewer deps). Its mock_nvim_web_devicons() shim keeps
-- plugins that still `require("nvim-web-devicons")` (barbar, nvim-tree,
-- lualine, which-key) working unchanged — the require is patched through
-- package.preload BEFORE they load.
return {
  {
    "echasnovski/mini.icons",
    lazy = true,
    opts = {},
    init = function()
      package.preload["nvim-web-devicons"] = function()
        require("mini.icons").mock_nvim_web_devicons()
        return package.loaded["nvim-web-devicons"]
      end
    end,
  },
}