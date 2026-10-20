-- treesitter-context: keep the enclosing function/signature visible while
-- scrolling long files (code reading). Lightweight; needs treesitter
-- parsers (already installed for go/ts/python/etc).
return {
  {
    "nvim-treesitter/nvim-treesitter-context",
    event = "BufReadPre",
    opts = {},
  },
}