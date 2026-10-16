-- Treesitter: parser infrastructure using the new (main branch) API.
-- Requires: nvim 0.12+, tree-sitter-cli in PATH, a C compiler.
return {
  {
    "nvim-treesitter/nvim-treesitter",
    lazy = false, -- plugin does not support lazy-loading
    build = ":TSUpdate",
    config = function()
      require("nvim-treesitter").setup({})

      local parsers = {
        "go", "gomod", "gosum",
        "typescript", "tsx", "javascript", "json",
        "lua", "vim", "vimdoc",
        "python", "yaml", "toml",
        "terraform", "hcl",
        "bash", "markdown", "dockerfile",
        "html", "css",
      }
      require("nvim-treesitter").install(parsers)

      -- Indentation via treesitter only for languages where it is stable;
      -- for the rest keep ftplugin defaults.
      local ts_indent_langs = { "lua", "go", "typescript", "typescriptreact", "javascript", "json", "toml" }

      -- Enable native treesitter highlighting for installed langs
      vim.api.nvim_create_autocmd("FileType", {
        pattern = parsers,
        callback = function(args)
          pcall(vim.treesitter.start)
          if vim.tbl_contains(ts_indent_langs, args.match) then
            vim.bo.indentexpr = "v:lua.require'nvim-treesitter'.indentexpr()"
          end
        end,
      })
    end,
  },
}
