-- Session management: resession.nvim — branch/worktree-aware sessions.
-- Fits the herdr worktree flow (per-branch checkouts; project names come
-- from the gitignored local/projects.env, see scripts/dotfiles-init.sh); `,ss` saves named sessions for the current directory+branch.
return {
  {
    "stevearc/resession.nvim",
    lazy = true,
    keys = {
      { "<leader>ss", function() require("resession").save() end, desc = "session: save" },
      { "<leader>sl", function() require("resession").load() end, desc = "session: load" },
      { "<leader>sd", function() require("resession").delete() end, desc = "session: delete" },
    },
    config = function()
      require("resession").setup({})
    end,
  },
}