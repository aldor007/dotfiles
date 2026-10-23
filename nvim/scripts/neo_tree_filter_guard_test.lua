-- Regression test: neo-tree finder mappings must survive state.tree == nil.
-- Upstream fuzzy_finder/filter_on_submit crash in renderer.get_expanded_nodes
-- when the async tree render hasn't completed (E5108, renderer.lua:631).
-- files.lua wraps s/S// with a nil-tree guard; this test pins that contract.
-- Run with: nvim --headless --clean -l scripts/neo_tree_filter_guard_test.lua
local failures = {}
local checks = 0

local function assert_true(cond, msg)
  checks = checks + 1
  if not cond then
    table.insert(failures, msg)
  end
end

-- Load the plugin spec (pure table construction; safe headless).
local spec_path = vim.fn.fnamemodify(debug.getinfo(1, "S").source:sub(2), ":h") .. "/../lua/plugins/files.lua"
local chunk, load_err = loadfile(spec_path)
assert_true(chunk ~= nil, "files.lua must load: " .. tostring(load_err))
if not chunk then
  error(table.concat(failures, "\n"))
end
local ok, spec = pcall(chunk)
assert_true(ok, "files.lua must execute: " .. tostring(spec))
if not ok then
  error(table.concat(failures, "\n"))
end

-- Locate the filesystem mappings table in the spec (nested one level deep
-- inside the plugin specs list, mirroring how lazy merges it).
local function find_filesystem(node)
  if type(node) ~= "table" then
    return nil
  end
  if node.filesystem ~= nil and type(node.filesystem) == "table" and node.filesystem.window ~= nil then
    return node.filesystem
  end
  for _, v in pairs(node) do
    local found = find_filesystem(v)
    if found then
      return found
    end
  end
  return nil
end
local fs = find_filesystem(spec)
assert_true(fs ~= nil, "spec must contain a filesystem section with window config")

-- 1. The three finder entry points must be guarded FUNCTIONS (not the bare
-- upstream command strings that crash on a nil tree).
local mappings = fs.window.mappings or {}
for _, key in ipairs({ "s", "S", "/" }) do
  assert_true(type(mappings[key]) == "function", "mapping '" .. key .. "' must be a guarded function, got " .. type(mappings[key]))
end

-- 2. Stub the upstream commands module BEFORE any mapping resolves it: calls
-- are recorded instead of executed, so no real tree/render is needed.
local upstream_calls = {}
package.loaded["neo-tree.sources.filesystem.commands"] = {
  fuzzy_finder = function(state)
    upstream_calls.fuzzy_finder = state
  end,
  fuzzy_finder_directory = function(state)
    upstream_calls.fuzzy_finder_directory = state
  end,
  filter_on_submit = function(state)
    upstream_calls.filter_on_submit = state
  end,
}

-- 3. THE REGRESSION: calling each mapping with a nil tree must not throw
-- (pre-fix this crashed inside renderer.get_expanded_nodes). Delegation is asserted by the aggregate check below.
for _, key in ipairs({ "s", "S", "/" }) do
  local ok_call, call_err = pcall(mappings[key], { tree = nil, config = {} })
  assert_true(ok_call, "mapping '" .. key .. "' with state.tree == nil must not error: " .. tostring(call_err))
end
assert_true(upstream_calls.fuzzy_finder == nil and upstream_calls.fuzzy_finder_directory == nil
  and upstream_calls.filter_on_submit == nil,
  "nil-tree guard must not reach upstream commands")

-- 4. With a tree present the guard must delegate to the upstream command,
-- preserving normal behavior.
mappings.s({ tree = {}, config = {} })
assert_true(upstream_calls.fuzzy_finder ~= nil, "mapping 's' with a tree must delegate to fuzzy_finder")
mappings.S({ tree = {}, config = {} })
assert_true(upstream_calls.fuzzy_finder_directory ~= nil, "mapping 'S' with a tree must delegate to fuzzy_finder_directory")
mappings["/"]({ tree = {}, config = {} })
assert_true(upstream_calls.filter_on_submit ~= nil, "mapping '/' with a tree must delegate to filter_on_submit")

print(string.format("neo-tree filter guard: %d checks, %d failures", checks, #failures))
if #failures > 0 then
  error(table.concat(failures, "\n"))
end
