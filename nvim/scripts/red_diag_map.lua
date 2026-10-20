-- One-shot helper: binds <F8> (insert + normal) to the blink-red diagnostic.
-- Usage:
--   :luafile ~/.config/nvim/scripts/red_diag_map.lua     (once per session)
-- Then: trigger the completion menu, press <F8> while it's open.
-- Results land in /tmp/blink-red-diag.txt (the menu stays open).
local diag = vim.fn.stdpath("config") .. "/scripts/blink_red_diag.lua"
for _, mode in ipairs({ "n", "i" }) do
  vim.keymap.set(mode, "<F8>", function()
    local chunk, err = loadfile(diag)
    if not chunk then
      vim.notify("diag load failed: " .. tostring(err), vim.log.levels.ERROR)
      return
    end
    local ok, run_err = pcall(chunk)
    if not ok then
      vim.notify("diag failed: " .. tostring(run_err), vim.log.levels.ERROR)
    end
  end, { desc = "dump completion UI diagnostics to /tmp" })
end
vim.notify("F8 diagnostic bound (insert + normal)", vim.log.levels.INFO)
