package tui

// Tests for review finding P1-1: the embedded tuicr subprocess must be
// terminated on every quit path — a normal UI exit may never leave tuicr
// running with a stale PTY.

import (
	"testing"
	"time"

	"mr-review/internal/plex"
)

// waitPaneChildExit polls the pane until cmd.Wait() has returned — the
// authoritative signal that the child process is gone (the exited channel
// closes only after Wait). Polls at 50ms intervals, well under the 100ms
// sleep cap mandated by the review.
func waitPaneChildExit(t *testing.T, p *plex.Pane) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for p.Alive() {
		if time.Now().After(deadline) {
			t.Fatal("tuicr child process still alive 5s after teardown")
		}
		time.Sleep(50 * time.Millisecond)
	}
	select {
	case <-p.Exited():
	default:
		t.Fatal("pane reports not-alive but Exited() is not closed")
	}
}

// openWsPane opens the embedded tuicr pane and returns the model plus the
// captured pane pointer (wsPane is cleared by teardown paths).
func openWsPane(t *testing.T) (*model, *plex.Pane) {
	t.Helper()
	wsFakeTuicr(t)
	m := newMultiFileModel(t, 160, 45)
	if m.rev.Worktree == "" {
		m.rev.Worktree = t.TempDir()
	}
	m2, _ := m.Update(key("W"))
	mm := m2.(*model)
	if wsPane == nil {
		t.Fatal("W must open the tuicr pane")
	}
	p := wsPane
	t.Cleanup(func() { mm.closeTuicrPane() }) // never leak panes between tests
	return mm, p
}

// TestWorkspaceCtrlCTerminatesTuicrChild — P1-1: Ctrl+C closes the pane on
// the model quit path and the child process actually terminates.
func TestWorkspaceCtrlCTerminatesTuicrChild(t *testing.T) {
	mm, p := openWsPane(t)

	m2, cmd := mm.Update(key("ctrl+c"))
	_ = m2
	if cmd == nil {
		t.Fatal("ctrl+c must quit the program")
	}
	if wsPane != nil {
		t.Fatal("ctrl+c must close the tuicr pane before quitting")
	}
	waitPaneChildExit(t, p)
}

// TestWorkspaceCloseTerminatesTuicrChild — W-close (closeTuicrPane) kills
// the child process too, not just the pointer.
func TestWorkspaceCloseTerminatesTuicrChild(t *testing.T) {
	mm, p := openWsPane(t)

	mm.closeTuicrPane()
	if wsPane != nil {
		t.Fatal("close must clear the pane")
	}
	waitPaneChildExit(t, p)
}
