-- Telescope: fuzzy finder (replaces CtrlP, unite, ack.vim)
return {
  {
    "nvim-telescope/telescope.nvim",
    cmd = "Telescope",
    dependencies = {
      "nvim-lua/plenary.nvim",
      { "nvim-telescope/telescope-fzf-native.nvim", build = "make" },
    },
    keys = {
      -- <C-p>: files in the CURRENT FILE'S DIRECTORY (monorepo-friendly);
      -- ,F for the whole worktree
      {
        "<C-p>",
        function()
          local ctx = require("config.context")
          local dir = ctx.source_dir()
          require("telescope.builtin").find_files({
            search_dirs = { dir },
            prompt_title = "Files [" .. dir .. "]",
          })
        end,
        desc = "Find files (context dir)",
      },
      { "<C-b>", "<cmd>Telescope buffers<cr>", desc = "Buffers" },
      -- <space>/: greps within the current file's directory (was project-wide
      -- before the monorepo scoping; ,G for the whole worktree)
      {
        "<space>/",
        function()
          local ctx = require("config.context")
          local dir = ctx.source_dir()
          require("telescope.builtin").live_grep({
            search_dirs = { dir },
            prompt_title = "Grep [" .. dir .. "]",
          })
        end,
        desc = "Grep (context dir)",
      },
      -- whole-worktree scopes (root = git worktree, .git dir or file)
      {
        "<leader>F",
        function()
          local root = require("config.context").worktree_root()
          require("telescope.builtin").find_files({
            search_dirs = { root },
            prompt_title = "Files (worktree) [" .. root .. "]",
          })
        end,
        desc = "Find files (worktree)",
      },
      {
        "<leader>G",
        function()
          local root = require("config.context").worktree_root()
          require("telescope.builtin").live_grep({
            search_dirs = { root },
            prompt_title = "Grep (worktree) [" .. root .. "]",
          })
        end,
        desc = "Grep (worktree)",
      },
      -- optional Go-module scope: nearest ancestor containing go.mod
      {
        "<leader>m",
        function()
          local dir = require("config.context").go_module_dir()
          if not dir then
            vim.notify("no go.mod near the current file", vim.log.levels.WARN)
            return
          end
          require("telescope.builtin").find_files({
            search_dirs = { dir },
            prompt_title = "Files (go module) [" .. dir .. "]",
          })
        end,
        desc = "Find files (go module)",
      },
      -- <space>g: fuzzy-pick a directory first, then live_grep inside it
      {
        "<space>g",
        function()
          local builtin = require("telescope.builtin")
          local actions = require("telescope.actions")
          local action_state = require("telescope.actions.state")

          local find_cmd = vim.fn.executable("fd") == 1
              and { "fd", "--type", "d", "--hidden", "--exclude", ".git", "--max-depth", "5" }
            or {
              "find", ".", "-type", "d",
              "-not", "-path", "*/.git/*",
              "-not", "-path", "*/node_modules/*",
            }

          builtin.find_files({
            prompt_title = "Pick directory to grep",
            find_command = find_cmd,
            previewer = false,
            attach_mappings = function(prompt_bufnr)
              actions.select_default:replace(function()
                local entry = action_state.get_selected_entry()
                local dir = entry and entry.value
                actions.close(prompt_bufnr)
                if dir then
                  builtin.live_grep({
                    prompt_title = "Grep [" .. dir .. "]",
                    search_dirs = { dir },
                  })
                end
              end)
              return true
            end,
          })
        end,
        desc = "Grep in directory",
      },
      -- <space>f: fuzzy-pick a directory first, then find files inside it
      {
        "<space>f",
        function()
          local builtin = require("telescope.builtin")
          local actions = require("telescope.actions")
          local action_state = require("telescope.actions.state")

          local find_cmd = vim.fn.executable("fd") == 1
              and { "fd", "--type", "d", "--hidden", "--exclude", ".git", "--max-depth", "5" }
            or { "find", ".", "-type", "d", "-not", "-path", "*/.git/*", "-not", "-path", "*/node_modules/*" }

          builtin.find_files({
            prompt_title = "Pick directory to search",
            find_command = find_cmd,
            previewer = false,
            attach_mappings = function(prompt_bufnr)
              actions.select_default:replace(function()
                local entry = action_state.get_selected_entry()
                local dir = entry and entry.value
                actions.close(prompt_bufnr)
                if dir then
                  builtin.find_files({
                    prompt_title = "Files in [" .. dir .. "]",
                    search_dirs = { dir },
                  })
                end
              end)
              return true
            end,
          })
        end,
        desc = "Find files in directory",
      },
      -- <space>cd: focus one service — fuzzy-pick a dir under <git-root>/services
      -- (or git root itself outside a monorepo) and :cd into it. nvim-tree follows
      -- (sync_root_with_cwd), all cwd-relative pickers scope down, and LSP servers
      -- re-root on next attach (:LspRestart for already-attached ones).
      {
        "<space>cd",
        function()
          local builtin = require("telescope.builtin")
          local actions = require("telescope.actions")
          local action_state = require("telescope.actions.state")

          local git_root = vim.fs.root(0, ".git") or vim.fn.getcwd()
          local services_dir = git_root .. "/services"
          local base = vim.uv.fs_stat(services_dir) and services_dir or git_root

          local find_cmd = vim.fn.executable("fd") == 1
              and { "fd", "--type", "d", "--max-depth", "1", ".", base }
            or { "find", base, "-mindepth", "1", "-maxdepth", "1", "-type", "d" }

          builtin.find_files({
            prompt_title = "Focus service (cd) [" .. git_root .. "]",
            find_command = find_cmd,
            previewer = false,
            attach_mappings = function(prompt_bufnr)
              actions.select_default:replace(function()
                local entry = action_state.get_selected_entry()
                local dir = entry and entry.value
                actions.close(prompt_bufnr)
                if dir then
                  dir = vim.fn.fnamemodify(dir, ":p")
                  vim.api.nvim_set_current_dir(dir)
                  print("cwd -> " .. dir)
                end
              end)
              return true
            end,
          })
        end,
        desc = "Focus service (cd)",
      },
      -- <space>cD: un-focus — cd back to the git root of the current buffer
      {
        "<space>cD",
        function()
          local root = vim.fs.root(0, ".git") or vim.fn.getcwd()
          vim.api.nvim_set_current_dir(root)
          print("cwd -> " .. root)
        end,
        desc = "cd back to git root",
      },
    },
    opts = function()
      return {
        defaults = {
          mappings = {
            i = { ["<esc>"] = require("telescope.actions").close },
          },
        },
        pickers = {
          find_files = {
            hidden = true,
            -- rg when available (fast, respects .gitignore); builtin find otherwise
            find_command = vim.fn.executable("rg") == 1
                and { "rg", "--files", "--hidden", "--glob", "!.git" }
              or nil,
          },
        },
      }
    end,
    config = function(_, opts)
      require("telescope").setup(opts)
      -- telescope is lazy-loaded; the notify extension hooks in when it is,
      -- per nvim-notify README (manual load for lazy-loaded telescope).
      pcall(require("telescope").load_extension, "notify")
    end,
  },

  -- telescope-file-browser: fuzzy file manager — create/delete/rename/move
  -- files+folders from a telescope picker (complements nvim-tree's tree;
  -- this is the fuzzy/rename workflow). Dep on telescope loads + setups
  -- first, then this config registers the extension.
  {
    "nvim-telescope/telescope-file-browser.nvim",
    dependencies = { "nvim-telescope/telescope.nvim", "nvim-lua/plenary.nvim" },
    keys = {
      { "<space>fb", "<cmd>Telescope file_browser<cr>", desc = "File browser (fuzzy)" },
    },
    config = function()
      require("telescope").load_extension("file_browser")
    end,
  },
}
