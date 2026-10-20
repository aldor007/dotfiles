-- blink-red diagnostic v2 — writes to /tmp/blink-red-diag.txt so the menu
-- never has to close. Bind with red_diag_map.lua, press F8 while the red
-- menu is open, then tell the agent (it reads the file directly).

local function resolve(name)
  local ok, hl = pcall(vim.api.nvim_get_hl, 0, { name = name, link = true })
  if not ok or not hl or not next(hl) then return "-" end
  local parts = {}
  if hl.fg then parts[#parts + 1] = string.format("fg=#%06x", hl.fg) end
  if hl.bg then parts[#parts + 1] = string.format("bg=#%06x", hl.bg) end
  if hl.sp then parts[#parts + 1] = string.format("sp=#%06x", hl.sp) end
  if hl.strikethrough then parts[#parts + 1] = "strike" end
  return table.concat(parts, " ")
end

local out = {}

local function emit(s)
  out[#out + 1] = s
end

local wins = vim.tbl_filter(function(w)
  return vim.api.nvim_win_is_valid(w)
    and vim.api.nvim_win_get_config(w).relative ~= ""
end, vim.api.nvim_tabpage_list_wins(0))

emit(("=== %d floating windows @ %s ==="):format(#wins, os.date("%H:%M:%S")))

for _, w in ipairs(wins) do
  local b = vim.api.nvim_win_get_buf(w)
  local cfg = vim.api.nvim_win_get_config(w)
  emit(string.format(
    "WIN %d buf=%d ft=%s zindex=%s winblend=%s relative=%s w=%d h=%d row=%s col=%s",
    w, b, vim.bo[b].filetype, tostring(cfg.zindex),
    tostring(vim.wo[w].winblend), tostring(cfg.relative),
    cfg.width or -1, cfg.height or -1, tostring(cfg.row), tostring(cfg.col)))
  emit("  winhighlight: " .. tostring(vim.wo[w].winhighlight))
  emit(string.format("  lines=%d", vim.api.nvim_buf_line_count(b)))
  for i, l in ipairs(vim.api.nvim_buf_get_lines(b, 0, -1, false)) do
    if l ~= "" then emit("    L" .. i .. ": [" .. l .. "]") end
  end
  local ms = vim.api.nvim_buf_get_extmarks(b, -1, 0, -1, { details = true })
  emit(string.format("  extmarks=%d", #ms))
  for _, m in ipairs(ms) do
    local d = m[4]
    if d and (d.hl_group or d.virt_text or d.virt_lines) then
      local extra = ""
      if d.virt_text then
        local segs = {}
        for _, seg in ipairs(d.virt_text) do
          segs[#segs + 1] = string.format("%q[%s]", seg[1], tostring(seg[2]))
        end
        extra = extra .. " vt=" .. table.concat(segs, ",")
      end
      if d.virt_lines then extra = extra .. " virt_lines=" .. #d.virt_lines end
      emit(string.format("    r%d c%d-%d eol=%s prio=%s %s [%s]%s",
        m[2], m[3], d.end_col or -1, tostring(d.hl_eol), tostring(d.priority),
        tostring(d.hl_group), resolve(d.hl_group), extra))
    end
  end
end

-- main window: mode, visual selection, and extmarks over the visible range
local mode_info = vim.fn.mode(true)
emit("=== main window ===")
emit("mode: " .. mode_info)
local cur = vim.fn.getcurpos()
emit(string.format("cursor: %d:%d", cur[2], cur[3]))
local vstart = vim.fn.getpos("'<")
local vend = vim.fn.getpos("'>")
emit(string.format("visual marks: '%d:%d -> %d:%d",
  vstart[2], vstart[3], vend[2], vend[3]))

local main = vim.api.nvim_get_current_win()
local mbuf = vim.api.nvim_win_get_buf(main)
local top = vim.fn.line("w0")
local bot = vim.fn.line("w$")
emit(string.format("visible range: %d-%d, extmarks in range:", top, bot))
local mms = vim.api.nvim_buf_get_extmarks(mbuf, -1, { top - 1, 0 }, { bot - 1, -1 },
  { details = true })
for _, m in ipairs(mms) do
  local d = m[4]
  if d and (d.hl_group or d.virt_text or d.virt_lines) then
    emit(string.format("    r%d c%d-%d eol=%s prio=%s %s [%s]",
      m[2], m[3], d.end_col or -1, tostring(d.hl_eol), tostring(d.priority),
      tostring(d.hl_group), resolve(d.hl_group)))
  end
end

-- red-capable global highlights
emit("=== suspect highlight groups ===")
for _, g in ipairs({ "Normal", "Visual", "VisualNOS", "Error", "ErrorMsg", "Search",
  "IncSearch", "CurSearch", "Substitute", "Pmenu", "PmenuSel", "PmenuKind",
  "PmenuKindSel", "PmenuExtra", "CursorLine", "CursorColumn", "DiffDelete",
  "TermCursor", "MatchParen", "EndOfBuffer" }) do
  emit(string.format("  %-14s %s", g, resolve(g)))
end

-- window-local matches (matchadd) — invisible to extmark dumps!
for _, w in ipairs(vim.list_slice(wins)) do
  local ms = vim.fn.getmatches(w)
  if ms and #ms > 0 then
    emit(string.format("MATCHES in win %d:", w))
    for _, m in ipairs(ms) do
      emit(string.format("    group=%s pattern=%s pos=%s prio=%s",
        tostring(m.group), tostring(m.pattern), vim.inspect(m.pos1 or m.pos), tostring(m.priority)))
    end
  end
end
local main_matches = vim.fn.getmatches(0)
if main_matches and #main_matches > 0 then
  emit("MATCHES in MAIN window:")
  for _, m in ipairs(main_matches) do
    emit(string.format("    group=%s pattern=%s prio=%s",
      tostring(m.group), tostring(m.pattern), tostring(m.priority)))
  end
end

-- live colors of ALL BlinkCmp* groups
emit("=== BlinkCmp* live colors ===")
for _, g in ipairs({ "BlinkCmpMenu", "BlinkCmpMenuBorder", "BlinkCmpMenuSelection",
  "BlinkCmpLabel", "BlinkCmpLabelMatch", "BlinkCmpLabelDescription", "BlinkCmpLabelDetail",
  "BlinkCmpLabelDeprecated", "BlinkCmpKind", "BlinkCmpKindText", "BlinkCmpKindSnippet",
  "BlinkCmpDoc", "BlinkCmpDocBorder", "BlinkCmpDocCursorLine", "BlinkCmpGhostText",
  "BlinkCmpSignatureHelp", "BlinkCmpSource", "BlinkCmpScrollBarThumb", "BlinkCmpScrollBarGutter" }) do
  emit(string.format("  %-28s %s", g, resolve(g)))
end

local ok, notify = pcall(require, "notify")
if ok then
  local hist = notify.history()
  emit(("=== %d notifications ==="):format(#hist))
  for i, n in ipairs(hist) do
    local msg = type(n.message) == "table" and table.concat(n.message, " / ")
      or tostring(n.message)
    emit(string.format("  %d [%s] %s", i, tostring(n.level), msg:sub(1, 140)))
  end
end

emit("=== end ===")
local f = io.open("/tmp/blink-red-diag.txt", "w")
f:write(table.concat(out, "\n") .. "\n")
f:close()
vim.notify("diag written to /tmp/blink-red-diag.txt", vim.log.levels.INFO,
  { title = "blink-red-diag" })
