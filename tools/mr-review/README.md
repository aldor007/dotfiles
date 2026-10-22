# mr-review

AI-assisted code review for GitLab merge requests, driven from the terminal.
A local agent (kimchi/pi) reviews the MR in an isolated worktree; you triage
its findings in a keyboard-first TUI, write your own comments, and publish
only what you explicitly approved.

**Core principle: AI assists; the human reviews, edits, approves and
explicitly authorizes publication.**

## Workflow

```
mr-review <MR-URL|IID>     # fetch MR → worktree → diff → TUI opens immediately
mr-review resume <dir>     # reopen an existing review (reattaches to a running AI review)
mr-review refresh <dir>    # MR moved: re-diff + re-review, decisions kept
mr-review export <dir>     # JSON/Markdown snapshot (local only, never publishes)
mr-review-publish <dir>    # post approved findings + ready comments (asks PUBLISH)
mr-review-publish approve <dir>  # formal MR approval (separate, explicit)
```

**The AI review runs in the background.** The TUI opens right away: browse
the continuous diff (view `2` → `Enter`), write manual comments, mark files
reviewed. While the detached agent works, the status line shows its progress
(`AI review running in background · N raw findings`); when it finishes, the
findings are ingested and appear in view `1` automatically. Closing the TUI
does not stop the review — `mr-review resume` reattaches to it.

**Hybrid workspace — mr-review owns the split.** `mr-review workspace <MR>`
runs one TUI with the AI triage cockpit on the left and a **real tuicr
running in an embedded PTY** on the right (built on charmbracelet/x/vt +
creack/pty — no tmux, no herdr, works in any terminal). `Ctrl+G` toggles
focus, `W` opens/closes the tuicr pane, the mouse works inside the tuicr
pane (SGR forwarding with pane-relative coordinates). Approved findings are
staged into the embedded session via `:` → "Stage approved findings in
tuicr".

## TUI layout

- **Left pane** — three views: AI findings (1), changed-file tree (2),
  comment navigator (3).
- **Right pane** — the continuous full-MR diff (tuicr-style: every changed
  file in one scrollable document with file headers, `j/k` walks it all) or
  the finding details with tabs: Diff · Explanation · Evidence · Comment ·
  Discussion.
- **Header** — MR identity, reviewed head SHA, decision counters.
- **Footer** — status line + contextual key hints.

## Keys (also discoverable in-app via `?` and `:`)

| Key | Action |
|---|---|
| `:` | searchable command palette (every action, same handlers as keys) |
| `?` | keyboard reference derived from the action registry |
| `j` `k` `gg` `G` | move · `Ctrl+d`/`Ctrl+u` half page |
| `Tab` / `Shift+Tab` | cycle panel focus · `Ctrl+h`/`Ctrl+l` left/right pane |
| `1` `2` `3` | left pane: findings / changed files / comment navigator |
| `h` `l` | detail tabs (when the right pane is focused) |
| `a` `r` `u` | approve / reject / undo an AI finding (local only) |
| `]p` `[p` | next / previous pending finding |
| `e` | edit the suggested comment (finding) or the selected draft (navigator) |
| `d` | discuss the finding with the AI (navigator: delete draft, confirmed) |
| `v` | open the code in Neovim (herdr pane when inside herdr) |
| `gd` | jump to the finding's line in the continuous full-MR diff |
| `{` `}` | previous / next file section · `[` `]` previous / next hunk (across files) |
| `c` | manual comment at the diff cursor / finding line |
| `V` | start a range selection in the diff (`j`/`k` extend, `c` comments) |
| `m` | toggle file reviewed · `R` toggle hunk reviewed (in the diff) |
| `gn` | jump to the next unreviewed file |
| `/` `n` `N` | search the focused view · next / previous match |
| `i` `z` | inspect the finding · zoom the details pane |
| `f` `o` | filter findings · cycle sort |
| `s` `S` | review summary · edit the summary comment ($EDITOR) |
| `x` | export dialog (JSON / Markdown, local write only) |
| `p` `L` | view the AI prompt / raw agent log |
| `P` | publication preview → publisher asks to type `PUBLISH` |
| `C` | remove the review worktree (state is kept) |
| `q` / `Ctrl+c` | quit (state is saved on every change) |
| `Esc` | cancel: selection, search, zoom, browse — never publishes |

## Manual comments

Comments you write are stored locally in `comments.jsonl`, independent of AI
findings. A draft is **ready** (offered for publication) or **held** (never
offered) — toggle in the navigator. Inline comments keep their exact diff
position (side, line, range); general MR comments have no position. After a
publish run the remote discussion ID is recorded and the draft is never
posted twice.

## Publication safety

- Only approved findings and ready comments are ever offered.
- The publisher re-verifies the MR head SHA first; a moved MR is refused
  (`mr-review refresh` first).
- Comments are de-duplicated against existing discussions.
- The final step always requires typing `PUBLISH` on a real terminal.
- Formal MR approval is a separate command with its own confirmation.
- Export never publishes and never mutates review state.

## State layout

State lives under `~/.local/share/mr-review/<project>/mr<iid>/`:
`review.json` (MR metadata, reviewed files/hunks), `findings.jsonl` (AI
findings + your decisions), `comments.jsonl` (manual drafts), `diff.patch`,
`prompt.txt`, `agent.log`, `discussions.json`, `publish-log.jsonl`. No
credentials are ever stored.

## Development

```
go build ./... && go test ./...
go test ./internal/tui -run TestGoldenSnapshots -update   # refresh goldens
```

Rendering is validated deterministically (no screenshots): frames are
asserted at 80×24, 100×30, 120×35, 160×45 and 200×50, with golden snapshots
for key screens.
