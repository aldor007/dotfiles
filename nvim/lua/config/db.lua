-- DB helpers: connect to CAST AI Cloud SQL via scripts/sql-db.sh.
-- Runs the script (starts the px IAP tunnel on demand), reads the raw DSN
-- from its machine mode (--dsn), sets g:db for vim-dadbod, and opens the
-- interactive console.
-- Note: IAM tokens rotate ~hourly — a failing query means re-run the keybind.
-- Real names/IDs (GCP projects, DB/instance names, ports) are read from the
-- gitignored local/projects.env (scripts/dotfiles-init.sh) — not from git.
local M = {}

-- Repo root resolved deterministically. The previous version gated on
-- `local/projects.env` (a generated, gitignored file) which means a
-- fresh clone with no projects.env falls back to `~/dotfiles` (or
-- whatever DOTFILES_DIR points to) even when those paths do not contain
-- the tracked scripts/sql-db.sh. Fix: probe in order of preference,
-- accepting the first location that owns the TRACKED marker
-- scripts/sql-db.sh. That file exists in the repo from day one.
--
-- Precedence (first match wins):
--   1. Valid explicit DOTFILES_DIR override whose scripts/sql-db.sh exists.
--   2. Repo derived from stdpath('config') via vim.uv.fs_realpath
--      (handles the ~/.config/nvim → <repo>/nvim symlink chain that
--      install.sh sets up), validated by the marker.
--   3. ~/dotfiles, validated by the marker.
-- If none has the marker, return nil — the caller surfaces a useful error.
local function find_repo_root()
  local candidates = {}
  if vim.env.DOTFILES_DIR and vim.env.DOTFILES_DIR ~= "" then
    table.insert(candidates, vim.env.DOTFILES_DIR)
  end
  local cfg = vim.fn.fnamemodify(vim.fn.stdpath("config"), ":p"):gsub("/+$", "")
  local real = vim.uv.fs_realpath(cfg) or cfg
  table.insert(candidates, vim.fn.fnamemodify(real, ":h"))
  table.insert(candidates, vim.fn.expand("~/dotfiles"))
  for _, candidate in ipairs(candidates) do
    if candidate and candidate ~= "" and vim.fn.filereadable(candidate .. "/scripts/sql-db.sh") == 1 then
      return candidate
    end
  end
  return nil
end

local ROOT = find_repo_root()
local SCRIPT = ROOT and (ROOT .. "/scripts/sql-db.sh") or nil
local ENV_FILE = ROOT and (ROOT .. "/local/projects.env") or nil
local EXAMPLE_ENV_FILE = ROOT and (ROOT .. "/scripts/projects.env.example") or nil
if not ENV_FILE and EXAMPLE_ENV_FILE and vim.fn.filereadable(EXAMPLE_ENV_FILE) == 1 then
  ENV_FILE = EXAMPLE_ENV_FILE
elseif ENV_FILE and vim.fn.filereadable(ENV_FILE) ~= 1 then
  ENV_FILE = nil
  if EXAMPLE_ENV_FILE and vim.fn.filereadable(EXAMPLE_ENV_FILE) == 1 then
    ENV_FILE = EXAMPLE_ENV_FILE
  end
end

if not ROOT then
  -- Surface the missing-marker error eagerly. Without this, every
  -- downstream call would fail with a less actionable message.
  vim.api.nvim_create_autocmd("VimEnter", {
    once = true,
    callback = function()
      local tried = {}
      if vim.env.DOTFILES_DIR then table.insert(tried, "DOTFILES_DIR=" .. vim.env.DOTFILES_DIR) end
      table.insert(tried, "repo from stdpath(config)")
      table.insert(tried, "~/dotfiles")
      vim.notify(
        "db: cannot locate repo root (no scripts/sql-db.sh found at " .. table.concat(tried, ", ") .. "). Run scripts/dotfiles-init.sh or set DOTFILES_DIR.",
        vim.log.levels.ERROR
      )
    end,
  })
end

local function load_env_file()
  local out = {}
  if ENV_FILE and vim.fn.filereadable(ENV_FILE) == 1 then
    for _, line in ipairs(vim.fn.readfile(ENV_FILE)) do
      local k, v = line:match("^([%w_]+)=(.*)$")
      if k and v then
        out[k] = v:gsub('^"', ""):gsub('"$', "")
      end
    end
  end
  return out
end

local env = load_env_file()
local ENVS = { "dev-master", "prod-master", "prod-eu", "prod-india" }
local DB_USER = env.DB_USER or "you@example.com"
local DB_NAME = env.DB_NAME or "app"

-- env name -> local tunnel port, from local/projects.env (env_<env>_port)
local function port_for_env(envname)
  local k = "env_" .. envname:gsub("-", "_") .. "_port"
  local v = env[k]
  return v and tonumber(v) or nil
end

---Mint ONE gcloud IAM token and register a connection for every env in
---g:dbs (same account serves all projects). No tunnel is started here —
---connections are listed but only usable once the px tunnel is up
---(,dD / scripts/sql-db.sh starts it). Re-seed after ~1h or on failure.
-- Canonical g:dbs table — owned here, assigned whole on change: vim.g reads
-- tables through the API and returns copies, so in-place mutation of
-- vim.g.dbs would silently never land in g:.
local dbs = {}
-- All public entrypoints (M.seed, M.check_tunnels, M.use_env, M.stop)
-- guard on these. None of them will crash with "attempt to concatenate
-- a nil value" — without this guard, M.seed's notify path would render
-- "db: nil missing/empty" which is correct but useless.
function M.seed()
  if not SCRIPT then
    vim.schedule(function()
      vim.notify(
        "db: cannot mint IAM token (no scripts/sql-db.sh found). " ..
        "Run scripts/dotfiles-init.sh or set DOTFILES_DIR.",
        vim.log.levels.ERROR
      )
    end)
    return
  end
  -- vim.system callbacks run in a fast-event context where vim.g, vim.notify
  -- and vim.cmd can hit E5560 ("vim.api.* must not be called in a fast event
  -- context"). vim.schedule_wrap defers the body to the main loop. Audit
  -- finding P1. The wrap also returns the job so callers can :wait().
  local on_done = vim.schedule_wrap(function(obj)
    if obj.code ~= 0 then
      return
    end
    local token = (obj.stdout or ""):gsub("%s+", "")
    if token == "" then
      return
    end
    local port_by_env = {}
    for _, e in ipairs(ENVS) do
      local p = port_for_env(e)
      if p then port_by_env[e] = p end
    end
    if next(port_by_env) == nil or #ENVS ~= vim.tbl_count(port_by_env) then
      local path = ENV_FILE or (ROOT and (ROOT .. "/local/projects.env")) or "<unknown>"
      vim.notify(
        "db: " .. path .. " missing/empty — run scripts/dotfiles-init.sh first",
        vim.log.levels.ERROR
      )
      return
    end
    for _, envname in ipairs(ENVS) do
      -- encode the FULL DB_USER (no hardcoded domain): IAM login uses the
      -- principal's own email as the username. The raw DSN must include the
      -- postgres:// scheme so vim-dadbod recognizes it as a valid connection.
      local user_enc = DB_USER:gsub("@", "%%40")
      dbs[envname] = string.format(
        "******127.0.0.1:%d/%s?sslmode=require",
        user_enc, token, port_by_env[envname], DB_NAME
      )
    end
    vim.g.dbs = dbs
    vim.notify("db: registered " .. #ENVS .. " connections (IAM token, ~1h)", vim.log.levels.INFO)
  end)
  return vim.system({ "gcloud", "auth", "print-access-token" }, { text = true }, on_done)
end

---Check which env tunnels are up and notify the exact command to bring
---missing ones up. px glass proxy is a TUI wizard that must run in a real
---terminal (sql-db.sh --fg); it cannot be backgrounded from nvim. Called on
---`User DBUIOpened`.
function M.check_tunnels()
  if not SCRIPT then
    return
  end
  local pending = #ENVS
  local down = {}
  -- Same fast-event context fix as M.seed.
  local on_result = vim.schedule_wrap(function(env, obj)
    if obj.code ~= 0 then
      down[#down + 1] = env
    end
    pending = pending - 1
    if pending == 0 and #down > 0 then
      vim.notify(
        "db: tunnel(s) down: " .. table.concat(down, ", ")
          .. " — start in a terminal:  sql-db.sh --env <env> --fg",
        vim.log.levels.WARN
      )
    end
  end)
  for _, env in ipairs(ENVS) do
    vim.system({ SCRIPT, "--env", env }, { text = true }, function(obj)
      on_result(env, obj)
    end)
  end
end

---Connect to <env> and open the dadbod console (async, non-blocking).
---Also registers the env in g:dbs so :DBUIToggle shows it (fresh token).
---@param env string
function M.use_env(env)
  if not SCRIPT then
    vim.schedule(function()
      vim.notify("db: cannot connect (no scripts/sql-db.sh found)", vim.log.levels.ERROR)
    end)
    return
  end
  dbs[env] = nil -- force a fresh resolve; re-added on success below
  local on_done = vim.schedule_wrap(function(obj)
    local out = (obj.stdout or "") .. (obj.stderr or "")
    if obj.code ~= 0 then
      vim.notify("sql-db: " .. out:gsub("%s+", " "):sub(1, 300), vim.log.levels.ERROR)
      return
    end
    -- --dsn prints ONLY the raw DSN (never echoed to a terminal by the script)
    local dsn = (obj.stdout or ""):gsub("%s+", "")
    if dsn == "" then
      vim.notify("sql-db: empty --dsn output", vim.log.levels.ERROR)
      return
    end
    vim.g.db = dsn
    dbs[env] = dsn -- :DBUIToggle will list this env while the token is valid
    vim.g.dbs = dbs
    vim.notify("db: connected to " .. env, vim.log.levels.INFO)
    pcall(vim.cmd, "DB")
  end)
  return vim.system({ SCRIPT, "--env", env, "--dsn" }, { text = true }, on_done)
end

---Tunnel teardown helper: scripts/sql-db.sh --stop (PID-file based, only
---kills tunnels this script started).
---
---Bug fixed: the previous version called vim.system() with NO on_exit
---callback and immediately vim.notify("db: tunnels stopped", INFO).
---That fired success BEFORE the script ran — and on failure it still
---claimed success. Correct shape: callback observes the exit code, and
---the notify runs ONLY after the process actually completes, scheduled
---on the main loop (vim.system callbacks run in a fast-event context
---where vim.notify / vim.api.* is forbidden — E5560).
function M.stop()
  if not SCRIPT then
    vim.schedule(function()
      vim.notify("db: cannot stop tunnels (no scripts/sql-db.sh found — install.sh not run?)",
        vim.log.levels.ERROR)
    end)
    return
  end
  local on_exit = vim.schedule_wrap(function(obj)
    -- Truncate stderr aggressively; never echo full DSNs (the script's
    -- --stop doesn't print them, but a future regression could).
    local stderr_safe = (obj.stderr or ""):gsub("%s+", " "):sub(1, 200)
    if obj.code ~= 0 then
      vim.notify("db: stop failed (exit " .. tostring(obj.code) .. ")"
        .. (stderr_safe ~= "" and (": " .. stderr_safe) or ""),
        vim.log.levels.ERROR)
      return
    end
    vim.notify("db: tunnels stopped (no tracked tunnels running)", vim.log.levels.INFO)
  end)
  vim.system({ SCRIPT, "--stop" }, { text = true }, on_exit)
end

return M