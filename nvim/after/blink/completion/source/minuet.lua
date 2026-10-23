-- blink.cmp source bridge for minuet-ai.nvim.
--
-- blink.cmp resolves completion sources via
-- `require('blink.completion.source.<name>')`. minuet-ai.nvim's init.lua
-- only registers with nvim-cmp (gated on `if has_cmp then ...`), so when
-- blink.cmp is the active engine — our case, per
-- nvim/lua/plugins/lsp.lua — the source is silently missing. blink.cmp
-- looks for it, finds nil, and skips it (no error, just no completions).
--
-- This file lives under `lua/after/` so nvim's runtime sources it AFTER
-- the main `lua/` tree, AFTER lazy.nvim has had a chance to load the
-- blink.cmp and minuet plugins. We delegate to `minuet.blink` (the
-- canonical source class for the blink.cmp integration that ships with
-- the plugin) via pcall so a missing or broken minuet install doesn't
-- take down blink.cmp's other sources — the source entry just resolves
-- to nil and blink.cmp silently skips it.
--
-- Result: with this shim in place, blink.cmp's `minuet` source resolves
-- to the real plugin-backed source on systems where minuet-ai.nvim is
-- installed, and stays out of the way elsewhere.
local ok, source = pcall(require, 'minuet.blink')
if ok then return source end
return nil
