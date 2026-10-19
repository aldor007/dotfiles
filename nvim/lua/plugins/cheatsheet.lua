-- Cheatsheet: :Cheatsheet / ,kh — a help popup listing every feature,
-- keymap and plugin in this config, with a live environment monitor.
-- Inspired by :Hardtime report (nui.nvim popup). Three data sources:
--   1. LIVE  — harvested keymaps, lazy plugin table, LSP/formatters/linters,
--              hardtime + notify + mason state
--   2. STATIC — curated hints accumulated across agent sessions (the things
--              actually asked about: hardtime popups, notify, k8s TUI,
--              monorepo focus, barbar gotchas, ...)
-- Scroll freely (hardtime counts j/k!); q or <Esc> closes.
return {
  name = "cheatsheet",
  dir = vim.fn.stdpath("config"), -- local plugin: lives in this config, nothing to clone
  cmd = { "Cheatsheet" },
  keys = {
    { "<leader>kh", "<cmd>Cheatsheet<cr>", desc = "cheatsheet: help popup" },
    { "<leader>?", "<cmd>Cheatsheet<cr>", desc = "cheatsheet: help popup" },
    { "?", "<cmd>Cheatsheet<cr>", desc = "cheatsheet: help popup" },
  },
  dependencies = { "MunifTanjim/nui.nvim" },
  config = function()
    local Popup = require("nui.popup")
    local event = require("nui.utils.autocmd").event

    ---------------------------------------------------------------------
    -- Quick start: fuzzy finder & git (telescope first)
    ---------------------------------------------------------------------
    local function quickstart_lines()
      return {
        "## Quick start — fuzzy finder & git",
        "",
        "-- Telescope (fuzzy finder · fzf-native sorter installed)",
        "  <C-p> files · <C-b> buffers · <Space>/ live grep · <Space>g grep in dir",
        "  <Space>f files in dir · <Space>cd focus service dir · <Space>cD back to git root",
        "  :Telescope notify — notification history · <Esc> closes pickers",
        "",
        "-- Git",
        "  fugitive: :Git (status, blame, merge) · :Gblame",
        "  diffview: ,go working tree · ,gl file history · ,gH repo history · ,gc close",
        "  gitsigns: ]h [h hunk jump · ,hs stage · ,hr reset · ,hp preview · ,gb ,gB blame",
        "",
      }
    end

    ---------------------------------------------------------------------
    -- Live environment monitor
    ---------------------------------------------------------------------
    local function monitor_lines()
      local L = {}

      local nvim_ver = tostring(vim.version())
      local startup = "n/a"
      local ok_stats, lazy_stats = pcall(function()
        return require("lazy.stats").stats()
      end)
      if ok_stats and lazy_stats and lazy_stats.startuptime
        and lazy_stats.startuptime > 0 then
        startup = string.format("%d ms", lazy_stats.startuptime)
      end

      local plugins, loaded, eager = 0, 0, 0
      local ok_lazy, lazy_cfg = pcall(require, "lazy.core.config")
      if ok_lazy and lazy_cfg and lazy_cfg.plugins then
        for _, spec in pairs(lazy_cfg.plugins) do
          plugins = plugins + 1
          if spec.lazy == false then eager = eager + 1 end
          if spec._ and spec._.loaded then loaded = loaded + 1 end
        end
      end

      local clients = {}
      for _, c in ipairs(vim.lsp.get_clients({ bufnr = 0 })) do
        clients[#clients + 1] = c.name
      end

      local formatters = {}
      local ok_conform, conform = pcall(require, "conform")
      if ok_conform then
        for _, f in ipairs(conform.list_formatters(0)) do
          formatters[#formatters + 1] = f.name
        end
      end

      local linters = {}
      local ok_lint, lint = pcall(require, "lint")
      if ok_lint then
        linters = lint.linters_by_ft[vim.bo.filetype] or {}
      end

      local hardtime_state = "off/not loaded"
      local ok_ht, ht_cfg = pcall(function()
        return require("hardtime.config").config
      end)
      if ok_ht and ht_cfg then
        hardtime_state = (ht_cfg.enabled and "enabled" or "disabled")
          .. (ht_cfg.disable_mouse and " · mouse off" or "")
      end

      local notify_count = 0
      local ok_notify, notify = pcall(require, "notify")
      if ok_notify then
        notify_count = #notify.history()
      end

      local mason_pkgs = 0
      local mason_dir = vim.fn.stdpath("data") .. "/mason/packages"
      local scan = vim.loop.fs_scandir(mason_dir)
      if scan then
        while true do
          local name = vim.loop.fs_scandir_next(scan)
          if not name then break end
          mason_pkgs = mason_pkgs + 1
        end
      end

      local branch = vim.fn.system({ "git", "rev-parse", "--abbrev-ref", "HEAD" })
      branch = vim.trim(branch or "")
      if vim.v.shell_error ~= 0 or branch == "" then branch = "-" end

      local function row(label, value)
        L[#L + 1] = string.format("  %-11s %s", label, value)
      end
      L[#L + 1] = "## Environment (live)"
      row("version", nvim_ver)
      row("startup", startup)
      row("plugins", string.format(
        "%d installed (%d eager, %d lazy) · %d loaded this session",
        plugins, eager, plugins - eager, loaded))
      row("lsp", #clients > 0 and table.concat(clients, ", ") or "none in this buffer")
      row("format", #formatters > 0 and table.concat(formatters, ", ") or "-")
      row("lint", #linters > 0 and table.concat(linters, ", ") or "-")
      row("hardtime", hardtime_state)
      row("notify", string.format("%d notifications in history", notify_count))
      row("mason", string.format("%d tools installed", mason_pkgs))
      row("git", branch .. " @ " .. vim.fn.fnamemodify(vim.fn.getcwd(), ":~"))
      L[#L + 1] = ""
      return L
    end

    ---------------------------------------------------------------------
    -- Plugin keys (live from each lazy spec's declared keys)
    ---------------------------------------------------------------------
    local function plugin_keys_lines()
      local L = { "## Plugin keys (declared in specs)", "" }
      local ok_lazy, lazy_cfg = pcall(require, "lazy.core.config")
      if not (ok_lazy and lazy_cfg and lazy_cfg.plugins) then
        L[#L + 1] = "  (lazy not available)"
        return L
      end

      local names = {}
      for name, spec in pairs(lazy_cfg.plugins) do
        if spec.keys and spec.keys ~= false and #spec.keys > 0 then
          names[#names + 1] = name
        end
      end
      table.sort(names)

      for _, name in ipairs(names) do
        local spec = lazy_cfg.plugins[name]
        L[#L + 1] = "### " .. name
        for _, k in ipairs(spec.keys) do
          local lhs, desc, modes
          if type(k) == "string" then
            lhs, desc, modes = k, "", "n"
          elseif type(k) == "table" and type(k[1]) == "string" and k[2] ~= false then
            lhs = k[1]
            desc = k.desc or ""
            modes = type(k.mode) == "table" and table.concat(k.mode, ",")
              or (k.mode or "n")
          else
            goto continue
          end
          -- only show keys with a clear description of what they do
          if desc == "" then goto continue end
          L[#L + 1] = string.format("  %-14s %-52s [%s]",
            lhs, desc, modes)
          ::continue::
        end
        L[#L + 1] = ""
      end

      L[#L + 1] = "-- In-buffer keys (buffer-local plugin UIs, not in specs)"
      L[#L + 1] = "  kubectl.nvim: g? help · gr refresh · gs sort · gd describe · gD delete"
      L[#L + 1] = "    gy yaml · ge edit · gk kill · <CR> drill down · <BS> up · <Tab>/<S-Tab> select"
      L[#L + 1] = "    gl logs · f follow · gh since · gP port-forwards · gp port-forward"
      L[#L + 1] = "    <C-x> contexts · <C-n> namespaces · <C-f> filter · 1-6 dep/pods/cm/secret/svc/ing"
      L[#L + 1] = "    grr rollout-restart · gss scale · gO/gU/gR cordon/uncordon/drain · F4 fullscreen"
      L[#L + 1] = "  other UIs (nvim-tree, dbui, dap-ui) have their own in-buffer help"
      L[#L + 1] = "  keys from lua/config/keymaps.lua appear in the Keymaps section below"
      L[#L + 1] = ""
      return L
    end

    ---------------------------------------------------------------------
    -- Live keymap harvest (grouped)
    ---------------------------------------------------------------------
    local function prettify(lhs, group)
      if group == "Leader" then return lhs end
      if group == "Space" then return "<Space>" .. lhs:sub(2) end
      return lhs
    end

    local function harvest_keymaps()
      local seen, maps = {}, {}
      for _, mode in ipairs({ "n", "i", "x", "v", "c", "t" }) do
        for _, km in ipairs(vim.api.nvim_get_keymap(mode)) do
          local lhs = vim.fn.keytrans(km.lhs)
          if
            lhs ~= ""
            and not lhs:find("Plug", 1, true)
            and not lhs:find("SNR", 1, true)
            and not lhs:find("Lua", 1, true)
            and not lhs:find("80>", 1, true)
            and km.desc ~= "which_key_ignore"
          then
            if not seen[lhs] then
              seen[lhs] = { lhs = lhs, desc = "", modes = {} }
              maps[#maps + 1] = seen[lhs]
            end
            seen[lhs].modes[mode] = true
            if km.desc and km.desc ~= "" then seen[lhs].desc = km.desc end
          end
        end
      end

      local order = { Leader = 1, Space = 2, Ctrl = 3, Alt = 4, g = 5, z = 6, tags = 7, other = 8 }
      local grouped = {}
      for _, m in ipairs(maps) do
        local g = "other"
        local d = m.lhs
        if d:sub(1, 1) == "," then g = "Leader"
        elseif d:sub(1, 1) == " " then g = "Space"
        elseif d:find("^<C%-") then g = "Ctrl"
        elseif d:find("^<M%-") or d:find("^<A%-") then g = "Alt"
        elseif d:find("^g") then g = "g"
        elseif d:find("^z") then g = "z"
        elseif d:find("^%]") or d:find("^%[") then g = "tags"
        end
        m.group = g
        m.display = prettify(m.lhs, g)
        grouped[g] = grouped[g] or {}
        table.insert(grouped[g], m)
      end

      local out = { "## Keymaps (harvested live)", "" }
      local titles = {
        Leader = "Leader (,) — the main namespace",
        Space = "<Space> — telescope / tabs",
        Ctrl = "Ctrl-*",
        Alt = "Alt-* (barbar tabs)",
        g = "g-prefixed motions & actions",
        z = "z-prefixed (folds/view)",
        tags = "[ ] motion tags",
        other = "Other",
      }
      local group_ids = {}
      for g in pairs(grouped) do group_ids[#group_ids + 1] = g end
      table.sort(group_ids, function(a, b) return (order[a] or 9) < (order[b] or 9) end)

      for _, g in ipairs(group_ids) do
        local entries = grouped[g]
        -- keep only keys with a clear description of what they do
        local described = {}
        for _, m in ipairs(entries) do
          if m.desc ~= "" then described[#described + 1] = m end
        end
        table.sort(described, function(a, b) return a.display:lower() < b.display:lower() end)
        out[#out + 1] = "### " .. titles[g] .. "  (" .. #described .. ")"
        for _, m in ipairs(described) do
          local modes = {}
          for _, mode in ipairs({ "n", "i", "x", "v", "c", "t" }) do
            if m.modes[mode] then modes[#modes + 1] = mode end
          end
          out[#out + 1] = string.format("  %-14s %-52s [%s]",
            m.display, m.desc, table.concat(modes, ""))
        end
        out[#out + 1] = ""
      end
      return out
    end

    ---------------------------------------------------------------------
    -- Plugin inventory (live from lazy)
    ---------------------------------------------------------------------
    local function plugin_lines()
      local L = { "## Plugins", "" }
      local ok_lazy, lazy_cfg = pcall(require, "lazy.core.config")
      if ok_lazy and lazy_cfg and lazy_cfg.plugins then
        local names = {}
        for name in pairs(lazy_cfg.plugins) do names[#names + 1] = name end
        table.sort(names)
        for _, name in ipairs(names) do
          local spec = lazy_cfg.plugins[name]
          local tag = spec.lazy == false and "eager" or "lazy"
          L[#L + 1] = string.format("  %-28s %s", name, tag)
        end
      end
      L[#L + 1] = "  full detail: :Lazy (ui) · which-key tree: press , or <Space> and wait"
      L[#L + 1] = ""
      return L
    end

    ---------------------------------------------------------------------
    -- Popup (nui, like :Hardtime report)
    ---------------------------------------------------------------------
    local popup

    local function close()
      if popup then
        pcall(function() popup:unmount() end)
        popup = nil
        vim.g.cheatsheet_bufnr = nil
      end
    end

    local function open()
      local lines = {}
      local function append(t)
        for _, l in ipairs(t) do lines[#lines + 1] = l end
      end
      append(quickstart_lines())
      append(plugin_keys_lines())
      append(harvest_keymaps())
      append(plugin_lines())
      append(monitor_lines()) -- environment last

      popup = Popup({
        enter = true,
        focusable = true,
        border = {
          style = "rounded",
          text = { top = " nvim cheatsheet — q/Esc closes ", top_align = "center" },
        },
        position = "50%",
        size = { width = math.floor(vim.o.columns * 0.85), height = math.floor(vim.o.lines * 0.88) },
        buf_options = { modifiable = false, buftype = "nofile", filetype = "cheatsheet" },
        win_options = { wrap = false, number = false, signcolumn = "no", spell = false },
      })

      popup:mount()
      vim.g.cheatsheet_bufnr = popup.bufnr
      vim.bo[popup.bufnr].modifiable = true
      vim.api.nvim_buf_set_lines(popup.bufnr, 0, -1, false, lines)
      vim.bo[popup.bufnr].modifiable = false

      local win = popup.winid
      pcall(vim.fn.matchadd, "Title", "^## .*", 10, -1, { window = win })
      pcall(vim.fn.matchadd, "Keyword", "^### .*", 10, -1, { window = win })
      pcall(vim.fn.matchadd, "Comment", "^%-%-.*", 10, -1, { window = win })
      pcall(vim.fn.matchadd, "Special", "`[^`]*`", 10, -1, { window = win })

      popup:map("n", "q", close)
      popup:map("n", "<Esc>", close)
      popup:on({ event.BufWipeout, event.BufLeave }, function()
        if popup then
          popup = nil
          vim.g.cheatsheet_bufnr = nil
        end
      end)
    end

    local function toggle()
      if
        popup
        and popup.winid
        and vim.api.nvim_win_is_valid(popup.winid)
      then
        close()
      else
        close() -- reap stale instance first
        open()
      end
    end

    vim.api.nvim_create_user_command("Cheatsheet", toggle, {
      desc = "Toggle the nvim cheatsheet popup (features, keymaps, hints)",
    })
  end,
}
