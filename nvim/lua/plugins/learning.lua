-- Learning tools: motion discovery + practice games that stay isolated from
-- normal code review and source edits.
return {
  {
    "tris203/precognition.nvim",
    cmd = { "Precognition" },
    keys = {
      { "<leader>ut", "<cmd>Precognition toggle<cr>", desc = "learning: toggle motion hints" },
      { "<leader>up", "<cmd>Precognition peek<cr>", desc = "learning: peek motion hints" },
    },
    opts = {
      startVisible = false,
      debounceMs = 0,
      showBlankVirtLine = false,
      highlightFullVirtLine = false,
      highlightColor = { link = "Comment" },
      targetedMotionHighlightColor = { link = "SpecialComment" },
      disabled_fts = {
        "neo-tree",
        "DiffviewFiles",
        "Outline",
        "TelescopePrompt",
        "lazy",
        "notify",
        "help",
        "qf",
      },
    },
  },
  {
    "ThePrimeagen/vim-be-good",
    cmd = { "VimBeGood", "VimBeGoodPractice" },
    keys = {
      { "<leader>uv", "<cmd>VimBeGoodPractice<cr>", desc = "learning: launch VimBeGood" },
    },
    config = function()
      vim.api.nvim_create_user_command("VimBeGoodPractice", function()
        -- Launch in a fresh tab so the exercise is isolated from any source file
        -- and never mutates the current work buffer.
        vim.cmd("tabnew")
        vim.cmd("enew")
        vim.cmd("VimBeGood")
      end, {
        desc = "Launch VimBeGood in a fresh empty tab without disturbing unsaved edits",
      })
    end,
  },
}
