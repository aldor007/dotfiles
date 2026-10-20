-- neo-tree: GoLand-style file explorer — the ONE file tree (replaces nvim-tree).
-- Default fs mappings (verified vs v3.x): a=create file, A=create dir,
-- r=rename, m=move, d=delete, y/x/p copy-cut-paste, <cr> open, ?=help,
-- /=fuzzy find, H=toggle hidden, f=filter on submit.
-- Icons come from mini.icons via a custom provider (no nvim-web-devicons).
return {
  {
    "nvim-neo-tree/neo-tree.nvim",
    branch = "v3.x",
    dependencies = { "nvim-lua/plenary.nvim", "MunifTanjim/nui.nvim" },
    cmd = { "Neotree" },
    keys = {
      -- <C-n> = the old nvim-tree/NERDTree toggle key: open/close the tree
      -- at the worktree root (nvim-tree's NvimTreeToggle behaviour)
      { "<C-n>", function() require("config.explorer").toggle_worktree() end, desc = "explorer: toggle (worktree)" },
      { "<leader>e", function() require("config.explorer").toggle_local() end, desc = "explorer: file context" },
      { "<leader>E", function() require("config.explorer").toggle_worktree() end, desc = "explorer: worktree root" },
    },
    config = function(_, opts)
      -- mini.icons must be configured BEFORE its first render-time use, or
      -- get() throws and the provider-error row crashes the renderer
      local ok, mini = pcall(require, "mini.icons")
      if ok and type(mini) == "table" and mini.icons == nil then
        pcall(mini.setup, {})
      end
      require("neo-tree").setup(opts)
    end,
    opts = {
      close_if_last_window = true,
      enable_git_status = true,      -- per-node git symbols in the tree
      enable_diagnostics = false,    -- diagnostics live in trouble
      window = { position = "left", width = 36 },
      events = {
        -- Filenames can contain a newline (agent-created junk); nui refuses
        -- multi-line items and the renderer crashed (nvim_buf_set_lines).
        -- Show the byte escaped so the entry stays visible AND openable
        -- (opening uses node.path, which we leave untouched).
        before_render = function(nodes)
          for _, node in ipairs(nodes) do
            if node and node.name and node.name:find("\n") then
              node.name = node.name:gsub("\n", "\\n")
            end
          end
        end,
      },
      default_component_configs = {
        icon = {
          -- provider using the already-installed mini.icons (no duplicate
          -- icon dependencies); must NEVER throw — neo-tree renders provider
          -- errors as tree rows, and error text with newlines crashes nui's
          -- set_lines ("replacement string item contains newlines").
          provider = function(icon, node)
            local ok, mini = pcall(require, "mini.icons")
            if not ok or type(mini) ~= "table" or not mini.get then
              return -- no provider -> neo-tree default icon
            end
            local okg, text, hl = pcall(mini.get,
              node.type == "directory" and "directory" or "file", node.name)
            if okg then
              if type(text) == "string" and text ~= "" then
                icon.text = text
              end
              if type(hl) == "string" and hl ~= "" then
                icon.highlight = hl
              end
            end
            -- pcall above: on ANY provider error we keep the default icon and
            -- neo-tree never renders an error row (which crashed nui: "item
            -- contains newlines")
          end,
        },
      },
      filesystem = {
        hijack_netrw_behavior = "open_default", -- :e dir opens neo-tree
        follow_current_file = {                 -- auto-reveal on buffer change
          enabled = true,
          leave_dirs_open = true,               -- keep expanded dirs expanded
        },
        use_libuv_file_watcher = true,          -- agent-created/changed files
        filtered_items = {
          hide_dotfiles = false,   -- visible by default; H toggles
          hide_gitignored = true,  -- respect .gitignore
          hide_ignored = true,
        },
      },
    },
  },
}