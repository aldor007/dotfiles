-- LSP: mason + native vim.lsp.enable API (nvim 0.11+) + blink.cmp completion
return {
  -- Mason: installs LSP servers (use :Mason to browse). Repo moved to mason-org.
  {
    "mason-org/mason.nvim",
    cmd = "Mason",
    build = ":MasonUpdate",
    opts = {},
  },

  -- Bridge: auto-installs LSP servers. Requires nvim-lspconfig in rtp at setup time,
  -- so lspconfig is a dependency (official recommended pattern). We keep
  -- automatic_enable = false and call vim.lsp.enable() ourselves in lspconfig's
  -- config so our vim.lsp.config() overrides are applied first.
  {
    "mason-org/mason-lspconfig.nvim",
    dependencies = {
      { "mason-org/mason.nvim", opts = {} },
      { "neovim/nvim-lspconfig" },
    },
    opts = {
      ensure_installed = { "gopls", "ts_ls", "lua_ls", "terraformls", "basedpyright", "yamlls" },
      automatic_enable = false,
    },
  },

  -- LSP config collection; enable via native vim.lsp.enable (0.11+ API)
  {
    "neovim/nvim-lspconfig",
    event = "BufReadPre",
    dependencies = {
      "mason-org/mason.nvim",
      "mason-org/mason-lspconfig.nvim",
    },
    config = function()
      -- yamlls is NOT enabled here — yaml-companion owns it (k8s.lua does
      -- vim.lsp.config("yamlls", cfg) + enable, so schemas come with it).
      local servers = { "gopls", "ts_ls", "lua_ls", "terraformls", "basedpyright" }

      vim.lsp.config("lua_ls", {
        settings = {
          Lua = {
            runtime = { version = "LuaJIT" },
            diagnostics = { globals = { "vim" } },
            workspace = {
              checkThirdParty = false,
              -- Don't scan vendored/legacy trees inside the dotfiles repo
              ignoreDir = { ".vim", "nvim-data", "node_modules" },
            },
          },
        },
      })

      for _, server in ipairs(servers) do
        vim.lsp.enable(server)
      end

      -- Keymaps when a server attaches
      vim.api.nvim_create_autocmd("LspAttach", {
        group = vim.api.nvim_create_augroup("UserLspConfig", {}),
        callback = function(args)
          local buf = args.buf
          local map = function(mode, lhs, rhs, desc)
            vim.keymap.set(mode, lhs, rhs, { buffer = buf, desc = desc })
          end
          map("n", "gd", vim.lsp.buf.definition, "Go to definition")
          map("n", "gD", vim.lsp.buf.declaration, "Go to declaration")
          -- gi/gr use Telescope pickers (preview + fuzzy filter), consistent
          -- with the other pickers below; single-select jumps directly.
          map("n", "gi", function()
            require("telescope.builtin").lsp_implementations()
          end, "Go to implementation")
          map("n", "gr", function()
            require("telescope.builtin").lsp_references()
          end, "Go to references (usages)")
          map("n", "K", vim.lsp.buf.hover, "Hover docs")
          map("n", "<leader>rn", vim.lsp.buf.rename, "Rename")
          map("n", "<leader>ca", vim.lsp.buf.code_action, "Code action")
          map("n", "]d", vim.diagnostic.goto_next, "Next diagnostic")
          map("n", "[d", vim.diagnostic.goto_prev, "Previous diagnostic")
          -- Telescope-backed LSP pickers (list + fuzzy filter, jump to result)
          map("n", "<leader>ld", function()
            require("telescope.builtin").diagnostics({ bufnr = 0 })
          end, "Document diagnostics")
          map("n", "<leader>lw", function()
            require("telescope.builtin").diagnostics()
          end, "Workspace diagnostics")
          map("n", "<leader>ls", function()
            require("telescope.builtin").lsp_document_symbols()
          end, "Document symbols (methods/functions)")
        end,
      })

      vim.diagnostic.config({
        virtual_text = true,
        signs = true,
        underline = true,
        update_in_insert = false,
      })
    end,
  },

  -- Completion: blink.cmp (Rust-based, replaces nvim-cmp + LuaSnip + sources).
  -- Pinned to stable v1 line (v2 is in development with breaking changes).
  {
    "saghen/blink.cmp",
    version = "1.*",
    event = "InsertEnter",
    dependencies = { "rafamadriz/friendly-snippets" },
    opts = {
      keymap = { preset = "default" },
      completion = {
        documentation = {
          auto_show = true,
          -- docs are rendered with the CURRENT buffer's treesitter parser;
          -- bash/terraform snippet bodies (with ${1:...} placeholders) parse
          -- into red @error noise — render plain text instead
          treesitter_highlighting = false,
        },
        accept = { auto_brackets = { enabled = true } },
      },
      signature = { enabled = true },
      sources = {
        default = { "lsp", "path", "snippets", "buffer", "minuet" },
        -- dadbod completion for SQL buffers (vim-dadbod-completion, db.lua)
        per_filetype = {
          sql = { "snippets", "dadbod", "buffer" },
        },
        providers = {
          dadbod = { name = "Dadbod", module = "vim_dadbod_completion.blink" },
          minuet = {
            name = "minuet",
            module = "minuet.blink",
            async = true,
            timeout_ms = 3000, -- matches minuet.request_timeout (s) * 1000
            score_offset = 50,
          },
        },
      },
      fuzzy = { implementation = "prefer_rust_with_warning" },
    },
  },
}
