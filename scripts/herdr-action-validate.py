#!/usr/bin/env python3
# scripts/herdr-action-validate.py
# Validate that action IDs referenced in herdr config.toml actually exist
# in the corresponding installed plugins. The herdr CLI's plugin action
# list returns JSON shaped like:
#   {"id": "cli:plugin", "result": {"actions": [{"action_id": "...", "plugin_id": "...", ...}]}}
#
# Usage from shell:
#   json=$(herdr plugin action list 2>/dev/null)
#   echo "$json" | python3 scripts/herdr-action-validate.py \
#     --plugin=kimchi-remote --expected=open-picker --expected=refresh-mine
# Exit code: 0 if every expected action was found in the given plugin,
#            1 otherwise.
#
# Usage from tests (synthetic manifest, no herdr binary required):
#   echo '{"result":{"actions":[{"action_id":"x","plugin_id":"p"}]}}' \
#     | python3 scripts/herdr-action-validate.py --plugin=p --expected=x
#
# Designed to be simple and shell-friendly: pure stdlib, no flags beyond
# --plugin and --expected=N, JSON parsed once at the top.
# Validate that action IDs referenced in herdr config.toml actually exist
# in the corresponding installed plugins. The herdr CLI's plugin action
# list returns JSON shaped like:
#   {"id": "cli:plugin", "result": {"actions": [{"action_id": "...", "plugin_id": "...", ...}]}}
#
# Usage from shell:
#   json=$(herdr plugin action list 2>/dev/null)
#   echo "$json" | python3 scripts/herdr-action-validate.py \
#     --plugin=kimchi-remote --expected=open-picker --expected=refresh-mine
# Exit code: 0 if every expected action was found in the given plugin,
#            1 otherwise.
#
# Usage from tests (synthetic manifest, no herdr binary required):
#   echo '{"result":{"actions":[{"action_id":"x","plugin_id":"p"}]}}' \
#     | python3 scripts/herdr-action-validate.py --plugin=p --expected=x
#
# Designed to be simple and shell-friendly: pure stdlib, no flags beyond
# --plugin and --expected=N, JSON parsed once at the top.

import argparse
import json
import sys

p = argparse.ArgumentParser(description="Validate herdr plugin actions")
p.add_argument("--plugin", required=True, action="append",
               help="Plugin ID whose actions to validate (may be repeated)")
p.add_argument("--expected", action="append", default=[],
               help="Action ID that must be present (may be repeated)")
args = p.parse_args()

raw = sys.stdin.read()
try:
    doc = json.loads(raw)
except json.JSONDecodeError as e:
    print(f"ERROR: could not parse herdr JSON output: {e}", file=sys.stderr)
    sys.exit(2)

actions = doc.get("result", {}).get("actions", [])
by_plugin = {}
for a in actions:
    by_plugin.setdefault(a.get("plugin_id", ""), set()).add(a.get("action_id", ""))

missing = []
seen_plugins = set()
for want in args.expected:
    want_plugin = None
    for plugin in args.plugin:
        if want in by_plugin.get(plugin, set()):
            want_plugin = plugin
            seen_plugins.add(plugin)
            break
    if want_plugin is None:
        # Identify which plugins were searched for a useful diagnostic.
        missing.append((want, list(args.plugin)))

if missing:
    print("install_herdr: VALIDATION FAILED — configured actions are missing "
          "from the installed plugin manifest(s):", file=sys.stderr)
    for action_id, plugins_searched in missing:
        print(f"  - {action_id} (not found in plugins: {', '.join(plugins_searched)})",
              file=sys.stderr)
    sys.exit(1)

# Per-plugin summary so the user sees what was actually validated.
for plugin in sorted(set(args.plugin)):
    count = len(by_plugin.get(plugin, set()))
    print(f"install_herdr: {plugin}: {count} actions registered, all expected present")

sys.exit(0)
