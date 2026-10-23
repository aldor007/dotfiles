-- DB helpers: connect to CAST AI Cloud SQL via scripts/sql-db.sh.
-- Runs the script (starts the px IAP tunnel on demand), reads the raw DSN
-- from its machine mode (--dsn), sets g:db for vim-dadbod, and opens the
-- interactive console.
-- Note: IAM tokens rotate ~hourly — a failing query means re-run the keybind.
-- Real names/IDs (GCP projects, DB/instance names, ports) are read from the
-- gitignored local/projects.env (scripts/dotfiles-init.sh) — not from git.
local M = {}

-- Repo root resolved from the live config dir. stdpath("config") is
-- the symlinked nvim/ inside the repo when installed via install.sh, so
-- we must resolve the SYMLINK to find the real repo root that owns
-- local/projects.env (without this, `local/projects.env` lookup fails
-- when nvim is symlinked from ~/.config/nvim → <repo>/nvim). DOTFILES_DIR
-- env overrides when running that config from elsewhere.
local function repo_root()
  local cfg = vim.fn.fnamemodify(vim.fn.stdpath("config"), ":p"):gsub("/+$", "")
  local real = vim.uv.fs_realpath(cfg) or cfg
  local parent = vim.fn.fnamemodify(real, ":h")
  if vim.fn.filereadable(parent .. "/local/projects.env") == 1 then
    return parent
  end
  return vim.env.DOTFILES_DIR or vim.fn.expand("~/dotfiles")
end

local ROOT = repo_root()
local SCRIPT = ROOT .. "/scripts/sql-db.sh"
local ENV_FILE = ROOT .. "/local/projects.env"

local function load_env_file()
  local out = {}
  if vim.fn.filereadable(ENV_FILE) == 1 then
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
function M.seed()
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
      vim.notify(
        "db: " .. ENV_FILE .. " missing/empty — run scripts/dotfiles-init.sh first",
        vim.log.levels.ERROR
      )
      return
    end
    for _, envname in ipairs(ENVS) do
      -- encode the FULL DB_USER (no hardcoded domain): IAM login uses the
      -- principal's own email as the username
      local user_enc = DB_USER:gsub("@", "%%40")
      dbs[envname] = string.format(
        "postgres://%s:%s@127.0.0.1:%d/%s?sslmode=require",
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
function M.stop()
  vim.system({ SCRIPT, "--stop" })
  vim.notify("db: tunnels stopped", vim.log.levels.INFO)
end

return M