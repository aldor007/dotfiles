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
      { "<leader>e", function() require("config.explorer").toggle_local() end, desc = "explorer: file context" },
      { "<leader>E", function() require("config.explorer").toggle_worktree() end, desc = "explorer: worktree root" },
    },
    opts = {
      close_if_last_window = true,
      enable_git_status = true,      -- per-node git symbols in the tree
      enable_diagnostics = false,    -- diagnostics live in trouble
      window = { position = "left", width = 36 },
      default_component_configs = {
        icon = {
          -- provider using the already-installed mini.icons (no duplicate
          -- icon dependencies); falls back to neo-tree defaults
          provider = function(icon, node)
            local ok, mini = pcall(require, "mini.icons")
            if not ok then return end
            local kind = node.type == "directory" and "directory" or "file"
            local text, hl = mini.icons.get(kind, node.name)
            if text then icon.text = text end
            if hl then icon.highlight = hl end
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