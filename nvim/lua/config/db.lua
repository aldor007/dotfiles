-- DB helpers: connect to CAST AI Cloud SQL via scripts/sql-db.sh.
-- Runs the script (starts the px IAP tunnel on demand), parses the printed
-- DSN, sets g:db for vim-dadbod, and opens the interactive console.
-- Note: IAM tokens rotate ~hourly — a failing query means re-run the keybind.
local M = {}

local SCRIPT = vim.fn.expand("~/dotfiles/scripts/sql-db.sh")

-- Keep in sync with scripts/sql-db.sh port_for_env()
local ENVS = { "dev-master", "prod-master", "prod-eu", "prod-india" }
local ENV_PORTS = { dev_master = 15432, prod_master = 15433, prod_eu = 15434, prod_india = 15435 }
local DB_USER = "marcin@cast.ai"
local DB_NAME = "service-a"

---Parse the DSN out of sql-db.sh output ("sql-db: dsn    postgres://...")
---@param out string
---@return string?
local function parse_dsn(out)
  return out:match("dsn%s+(%S+)")
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
  local job = vim.system({ "gcloud", "auth", "print-access-token" }, { text = true }, function(obj)
    if obj.code ~= 0 then
      return
    end
    local token = (obj.stdout or ""):gsub("%s+", "")
    if token == "" then
      return
    end
    local port_by_env = {}
    for k, port in pairs(ENV_PORTS) do
      port_by_env[k:gsub("%_", "-")] = port
    end
    for _, env in ipairs(ENVS) do
      dbs[env] = string.format(
        "postgres://%s%%40cast.ai:%s@127.0.0.1:%d/%s?sslmode=disable",
        DB_USER:gsub("@.*", ""), token, port_by_env[env], DB_NAME
      )
    end
    vim.g.dbs = dbs
    vim.notify("db: registered " .. #ENVS .. " connections (IAM token, ~1h)", vim.log.levels.INFO)
  end)
  return job
end

---Connect to <env> and open the dadbod console (async, non-blocking).
---Also registers the env in g:dbs so :DBUIToggle shows it (fresh token).
---@param env string
function M.use_env(env)
  dbs[env] = nil -- force a fresh resolve; re-added on success below
  local job = vim.system({ SCRIPT, "--env", env }, { text = true }, function(obj)
    local out = (obj.stdout or "") .. (obj.stderr or "")
    if obj.code ~= 0 then
      vim.notify("sql-db: " .. out:gsub("%s+", " "):sub(1, 300), vim.log.levels.ERROR)
      return
    end
    local dsn = parse_dsn(out)
    if not dsn then
      vim.notify("sql-db: no DSN in output: " .. out:gsub("%s+", " "):sub(1, 200), vim.log.levels.ERROR)
      return
    end
    vim.g.db = dsn
    dbs[env] = dsn -- :DBUIToggle will list this env while the token is valid
    vim.g.dbs = dbs
    vim.notify("db: connected to " .. env, vim.log.levels.INFO)
    pcall(vim.cmd, "DB")
  end)
  return job
end

---Tunnel teardown helper: scripts/sql-db.sh --stop
function M.stop()
  vim.system({ SCRIPT, "--stop" })
  vim.notify("db: tunnel stopped", vim.log.levels.INFO)
end

return M