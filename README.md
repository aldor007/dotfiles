# dotfiles

Personal dotfiles. Primary shell: **zsh** (+ oh-my-zsh). Editor: **Neovim**
(config under [`nvim/`](nvim/), validated by `nvim/scripts/validate.sh`).
Terminal multiplexer: **tmux**. Workflows: **herdr**-managed panes/workspaces.

## Layout

| Path | What |
|---|---|
| `nvim/` | Neovim config (lazy.nvim). Plugin list + docs live with the config; `,kk` lists every keymap. Validate with `nvim/scripts/validate.sh` |
| `nvim/` formatters | `conform.nvim` formats on save (LSP fallback), `nvim-lint` adds ruff/shellcheck. Binaries: `brew install stylua ruff prettier shellcheck` or `./install.sh -i formatters` |
| `.zshrc` | oh-my-zsh, PATH/tool wiring (gsd, kimchi/pi, docker wrapper, PR summary) |
| `.tmux/` | tmux config (prefix `C-a`, mouse on, optional powerline2) |
| `scripts/` | `pr-summary.sh` (daily PR/MR digest), `worktree-clean`, `service-repo-project`, `pi` (kimchi shim), `sql-db.sh` (per-env Cloud SQL via px IAP tunnel + IAM — `--psql`, `--rainfrog`, `--visql`, `--fg`), small utils |
| `.config/herdr/` | herdr plugin configs (single source of truth, symlinked in by install.sh) |
| `install.sh` | installer — `-i herdr` (any OS), `-i all` (legacy Debian/Ubuntu stack) |
| `.gitconfig` `.pylintrc` `.eslintrc.json` `.jshintrc` `.screenrc` | classic dotfiles |

## Install

```bash
# herdr + plugin configs (works on macOS and Linux)
./install.sh -i herdr

# formatters/linters for nvim (conform.nvim + nvim-lint)
./install.sh -i formatters

# rainfrog DB TUI (brew/cargo) + repo-managed config
./install.sh -i rainfrog

# vi-sql vim-mode SQL TUI (brew; includes an MCP server for AI tools)
./install.sh -i vi-sql

# legacy full stack — Debian/Ubuntu only (apt-based, includes nvim-era tools)
./install.sh -i all
```

> The historical `curl install.sh | bash` one-liner installs the legacy Linux
> stack; on macOS only the `herdr` target is supported. Secrets live in
> `~/.secrets.zsh` (outside this repo), sourced from `.zshrc`.

## Scripts

- `scripts/pr-summary.sh` — open-PR/MR digest with CI state, merge conflicts
  and new comments since the last run; wire up via the `prs`/`prsa` aliases.
- `scripts/worktree-clean` — safe removal of inactive git worktrees.
- `scripts/service-repo-project` — worktree-aware project opener for service-repo.
- `scripts/pi` — `pi` -> `kimchi` shim so herdr's native session restore works.