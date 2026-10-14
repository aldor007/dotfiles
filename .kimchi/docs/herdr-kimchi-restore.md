# Fix: kimchi agents untracked after herdr session restore (2026-10-14)

## Symptom
After a herdr server restart / session restore, panes that ran kimchi came back as
plain shells: no agent in the sidebar, no `idle/working` state, no resumed session.
Discovery was a herdr restart at 05:56 (`persist.restore ok, workspaces=21`) which
respawned every pane as a fresh shell; all 11 stored agent-side session refs were
later revived manually between 07:57–08:19 (visible in `ps` timestamps — native
restore fans out at ~100 ms per agent, so staggered minutes = manual).

## Root cause
1. Kimchi is tracked via herdr's **pi integration**
   (`~/.config/kimchi/harness/extensions/herdr-agent-state.ts`, installed into the
   kimchi harness through `PI_CODING_AGENT_DIR`). Two launch flavors existed:
   - **restore-eligible**: launched as `env HERDR_AGENT=pi KIMCHI_AGENT_SOURCE=herdr:pi KIMCHI_AGENT_NAME=pi kimchi`
     (herdr-plus TOMLs, herdr-jira delegate) → reports with official source
     `herdr:pi` + `agent_session_path` (jsonl) → persisted in `session.json`.
   - **non-eligible**: plain `kimchi` / `pi` inside a pane → fallback
     `agent=kimchi`, `source=kimchi-bridge` → tracked live but never resumed.
2. herdr's native restore (`[session] resume_agents_on_restore = true`, default)
   resumes eligible panes with the canonical Pi command **`pi --session <path>`** —
   and **no `pi` binary existed on PATH** (only `~/.local/bin/kimchi`). herdr docs:
   the resume command's first word must be a plain command on PATH.
3. The integration itself was outdated: v8 while herdr 0.9.3 wanted v9.

## Fix
- **`scripts/pi` shim** (`exec kimchi "$@"`) symlinked to `~/.local/bin/pi`
  by `install.sh`; guarantees the canonical `pi --session <path>` resolves to the
  kimchi fork, which accepts the identical flag.
- **`.zshrc kimchi()`**: inside herdr panes (`HERDR_ENV=1`) launches
  `env HERDR_AGENT=pi KIMCHI_AGENT_SOURCE=herdr:pi KIMCHI_AGENT_NAME=pi kimchi ...`
  so manually started panes also store a restore-eligible `herdr:pi` session ref.
- **`herdr integration install pi`**: v8 → v9.
- Commits: `8a4e011` (shim + install.sh + .zshrc), doc commit (this file).

### What v9 changed (diff v8→v9)
- Comments rebranded `herdr-kimchi` → `herdr`; `HERDR_INTEGRATION_VERSION=9`.
- **source/agent are now hardcoded** `source="herdr:pi"`, `agent="pi"` — the
  `KIMCHI_AGENT_SOURCE`/`KIMCHI_AGENT_NAME` env overrides are now inert (harmless
  to keep setting them; they also still carry `HERDR_AGENT=pi` process hints).
- Session-path check broadened (win32 absolute paths). **No `resume_argv`** —
  resume stays the canonical `pi --session`.

## Verification evidence (this session)
- `command -v pi` resolves (~/.local/bin/pi); shim forwards (`pi --version` → 1.9.2).
- `herdr integration status` → `pi: current (v9)`.
- Named-session round trip (`herdr --session kimchi-restore-test`):
  1. Fresh pane ran plain `kimchi` → `herdr agent list` showed `agent=pi` with
     `agent_session{source:herdr:pi, kind:path}` ref — chunk-2 path works as-is.
  2. `herdr session stop kimchi-restore-test`, server restarted.
  3. Without any manual revive, herdr spawned
     `kimchi --enable-experimental-features --session <same-jsonl>` (pid observed
     via `ps`, started seconds after server start inside the resume window) and
     the pane listed as tracked `pi` with hook-authority reporting
     (`agent explain`: `full_lifecycle_hook_authority`). The pane showed the
     resumed kimchi UI.
  4. Session deleted afterwards.
- Note: herdr's resume spawn goes through the pane's login zsh — argv on the
  resumed process carried our function's `--enable-experimental-features` flag,
  proving `.zshrc` participates in the resumed environment.

## Standing rules
- Kimchi inside herdr is always launched with `HERDR_AGENT=pi KIMCHI_AGENT_SOURCE=herdr:pi KIMCHI_AGENT_NAME=pi` (TOMLs and `.zshrc kimchi()` both do this).
- Panes launched before this fix keep `kimchi-bridge`/no refs until relaunched.
- `scripts/pi` shadows nothing today; revisit only if the real Pi agent is installed.

## Follow-ups
- `herdr-reviewr` binary contains no kimchi strings → it does not spawn kimchi;
  if reviewer panes still lose tracking, apply the same env convention at that
  launch site (TBD with owner).
- Next real herdr restart: expect all env-launched panes to resume unaided
  (observational regression check).
