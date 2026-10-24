# Neovim learning workflow

This config includes a lightweight learning layer for motion practice, key discovery, and safe exercise sessions without disturbing normal code review.

## Plugins in the loop

- `precognition.nvim`: subtle motion hints for the active buffer. It stays hidden by default and can be toggled on only when you want guidance.
- `hardtime.nvim`: gentle feedback for repeated motion keys and bad habits without blocking normal navigation.
- `vim-be-good`: isolated Vim movement exercises in a fresh empty buffer.
- `which-key.nvim`: leader-based hints for the learning actions and keymap discovery.

## New shortcuts

- `,um` — toggle learning mode on/off
- `,ut` — toggle Precognition motion hints
- `,up` — peek at motion hints for the current cursor position
- `,uh` — toggle Hardtime feedback
- `,ur` — open the Hardtime report
- `,uv` — launch VimBeGood practice in a fresh tab
- `,kk` — list all keymaps
- `,kh` or `,?` — open the keymap/help cheatsheet

## Learning mode

Normal mode is the default: motion hints are off, Hardtime is mostly quiet, and code review remains unchanged.

Learning mode keeps the experience gentle and reversible:

- `,um` turns on motion hints and Hardtime feedback.
- `,um` again turns it back off without restarting Neovim.
- `,ut` lets you toggle hints independently if you want motion discovery without a full mode change.
- `,up` briefly reveals motion hints for a moment without leaving them permanently enabled.

This is useful while reading Go code and moving between functions, imports, and comments without hunting for the right motion.

## Launching practice exercises

- Run `,uv` to open VimBeGood in a dedicated practice tab.
- The practice tab is intentionally separate from your working files, so unsaved edits stay untouched.
- Re-running `,uv` reuses the same practice tab instead of stacking more tabs; use `:tabclose` or close the tab to exit.
- Relaunch with `,uv` when you want to restart the same practice loop.

## Hardtime report

- `,ur` opens the Hardtime report.
- The report highlights repeated keys and common bad habits.
- It is useful for a quick review after a 10-minute practice block.

## Recommended 10-minute routine

1. Open a Go file and read for a minute while using normal navigation.
2. Toggle learning mode with `,um` and keep Precognition visible while moving between functions and statements.
3. Practice `hjkl`, word motions, target jumps, search, and accurate cursor placement using VimBeGood.
4. Close the exercises by leaving the practice tab (for example, `:tabclose`).
5. Turn off learning mode with `,um` and return to normal review.

## Returning to normal mode

- Press `,um` to disable learning mode.
- The hints disappear and Hardtime stops issuing gentle feedback.
- Your normal review and LSP/Telescope/Diffview workflows remain unchanged.
