-- DB helpers: connect to CAST AI Cloud SQL via scripts/sql-db.sh.
-- Runs the script (starts the px IAP tunnel on demand), parses the printed
-- DSN, sets g:db for vim-dadbod, and opens the interactive console.
-- Note: IAM tokens rotate ~hourly — a failing query means re-run the keybind.
local M = {}

local SCRIPT = vim.fn.expand("~/dotfiles/scripts/sql-db.sh")

---Parse the DSN out of sql-db.sh output ("sql-db: dsn    postgres://...")
---@param out string
---@return string?
local function parse_dsn(out)
  return out:match("dsn%s+(%S+)")
end

---Connect to <env> and open the dadbod console (async, non-blocking).
---Also registers the env in g:dbs so :DBUIToggle shows it (fresh token).
---@param env string
function M.use_env(env)
  vim.g.dbs = vim.g.dbs or {}
  vim.g.dbs[env] = nil -- force a fresh resolve; re-added on success below
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
    vim.g.dbs[env] = dsn -- :DBUIToggle will list this env while the token is valid
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