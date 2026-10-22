// tuicrstage.go — TUI wiring for staging approved findings into tuicr.
// The staging itself lives in internal/tuicr (CLI-based, local session store
// only — no GitLab writes here).
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"mr-review/internal/store"
	"mr-review/internal/tuicr"

	tea "charm.land/bubbletea/v2"
)

type tuicrDoneMsg struct {
	note string
	err  error
}

func (m *model) tuicrClient() *tuicr.CLI { return tuicr.NewCLI() }

func (m *model) tuicrOpts() tuicr.StageOptions {
	wt := m.rev.Worktree
	if wt == "" {
		wt, _ = os.Getwd()
	}
	return tuicr.StageOptions{
		Dir:      m.dir,
		RepoPath: wt,
		Project:  m.rev.Project,
		IID:      m.rev.IID,
		Username: "mr-review AI (human-approved)",
	}
}

// stageTuicrCmd stages approved findings into the tuicr session (background
// cmd; the result lands as a status message — no focus stealing).
func (m *model) stageTuicrCmd() tea.Cmd {
	opts := m.tuicrOpts()
	cli := m.tuicrClient()
	return func() tea.Msg {
		res, err := tuicr.StageApproved(context.Background(), opts, cli)
		note := ""
		if res != nil {
			note = fmt.Sprintf("staged %d, skipped %d, failed %d",
				len(res.Staged), len(res.Skipped), len(res.Failed))
			if len(res.Failed) > 0 {
				note += " · " + strings.Join(res.Failed, "; ")
			}
		}
		return tuicrDoneMsg{note: note, err: err}
	}
}

// syncTuicrCmd reads the session back and reports human edits/deletions.
// tuicr text is authoritative once staged — nothing is rewritten.
func (m *model) syncTuicrCmd() tea.Cmd {
	opts := m.tuicrOpts()
	cli := m.tuicrClient()
	return func() tea.Msg {
		edited, deleted, err := tuicr.SyncState(context.Background(), opts, cli)
		if err != nil {
			return tuicrDoneMsg{err: err}
		}
		note := fmt.Sprintf("staged comments intact · %d edited by you, %d deleted (tuicr text wins)", len(edited), len(deleted))
		if len(edited) > 0 {
			note = fmt.Sprintf("you edited staged comments: %s — tuicr text wins (never overwritten)", strings.Join(edited, ", "))
		}
		return tuicrDoneMsg{note: note}
	}
}

func (m *model) handleTuicrDone(msg tuicrDoneMsg) {
	if msg.err != nil {
		m.msg = "tuicr: " + msg.err.Error()
		return
	}
	m.msg = "tuicr: " + msg.note
}

var _ = store.Now // theme parity with other files (kept import used)
