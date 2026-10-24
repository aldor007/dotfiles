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
      -- vim-be-good assumes a UI exists and dereferences
      -- vim.api.nvim_list_uis()[1]. In headless validation, that list is empty,
      -- so provide a fallback geometry from the current editor dimensions.
      local list_uis = vim.api.nvim_list_uis
      vim.api.nvim_list_uis = function()
        local uis = list_uis()
        if #uis > 0 then
          return uis
        end
        return {
          {
            width = vim.o.columns or 120,
            height = vim.o.lines or 40,
          },
        }
      end

      local function ensure_practice_tab()
        local tabnr = vim.g.vim_be_good_practice_tab
        if tabnr and vim.api.nvim_tabpage_is_valid(tabnr) then
          vim.api.nvim_set_current_tabpage(tabnr)
          return tabnr
        end

        vim.cmd("tabnew")
        local new_tab = vim.api.nvim_get_current_tabpage()
        vim.g.vim_be_good_practice_tab = new_tab
        return new_tab
      end

      vim.api.nvim_create_user_command("VimBeGoodPractice", function()
        -- Reuse a dedicated practice tab so repeated launches do not pile up
        -- tabs or disturb unsaved source buffers elsewhere in the session.
        local tabnr = ensure_practice_tab()
        local current = vim.api.nvim_get_current_tabpage()
        if current ~= tabnr then
          vim.api.nvim_set_current_tabpage(tabnr)
        end
        vim.cmd("enew")
        vim.cmd("VimBeGood")
      end, {
        desc = "Launch VimBeGood in a dedicated practice tab without disturbing unsaved work",
      })
    end,
  },
}
