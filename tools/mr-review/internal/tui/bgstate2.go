// bgreview.go — TUI side of the background AI review: a 2s poll of
// agent.state shows progress while the user reviews the diff manually; when
// the detached review finishes, findings are reloaded into the list
// automatically (deferred while an editor is open, applied on close).
package tui

import (
	"fmt"
	"time"

	"mr-review/internal/store"

	tea "charm.land/bubbletea/v2"
)

// bgTickMsg drives the background-review poll.
type bgTickMsg struct{}

func bgTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return bgTickMsg{} })
}

// bgPoll returns the next poll command while a background review is being
// tracked (running, or finished but not yet reloaded).
func (m *model) bgPoll() tea.Cmd {
	if m.bgReview == nil {
		return nil
	}
	st := *m.bgReview
	if st.Status == store.AgentRunning || st.Status == store.AgentDone && !m.bgLoaded {
		return bgTick()
	}
	return nil
}

// refreshBGState reads the heartbeat and reports whether the findings list
// must be reloaded (first observation of a done state, or a state file that
// disappeared).
func (m *model) refreshBGState() (reload bool) {
	st := store.ReadAgentState(m.dir)
	if st == nil {
		if m.bgReview != nil {
			m.bgReview = nil
			reload = true
		}
		return reload
	}
	resolved := st.Resolved(m.dir)
	prev := m.bgReview
	m.bgReview = &resolved
	m.bgRaw = resolved.Raw
	switch {
	case prev == nil && resolved.Status == store.AgentDone:
		reload = true // finished while we were away
	case prev != nil && prev.Status == store.AgentRunning && resolved.Status == store.AgentDone:
		reload = true
	}
	return reload
}

// bgRunning reports whether an AI review is currently executing detached.
func (m *model) bgRunning() bool {
	return m.bgReview != nil && m.bgReview.Status == store.AgentRunning
}

// bgStatusLine renders the background-review status text.
func (m *model) bgStatusLine() string {
	st := m.bgReview
	if st == nil {
		return ""
	}
	switch st.Status {
	case store.AgentRunning:
		line := fmt.Sprintf("%s AI review running in background · %s elapsed · %d raw findings",
			m.spin.View(), m.bgElapsed(), m.bgRaw)
		if n := len(m.findings); n > 0 {
			line += fmt.Sprintf(" · %d loaded", n)
		}
		if m.bgActivity != "" {
			line += " · " + truncPlain(m.bgActivity, 60)
		}
		return styleWarn.Render(line)
	case store.AgentFailed:
		return styleErr.Render("Status: background AI review FAILED — " + st.Error +
			" (retry: : → Refresh review)")
	case store.AgentInterrupted:
		return styleWarn.Render("Status: background AI review was interrupted — retry with : → Refresh review")
	case store.AgentDone:
		return styleOK.Render(fmt.Sprintf("Status: AI review finished · %d findings loaded", st.Findings))
	}
	return ""
}

// bgPendingReload applies a deferred findings reload once editors are closed.
func (m *model) bgPendingReload() tea.Cmd {
	if !m.pendingReload {
		return nil
	}
	m.pendingReload = false
	if err := m.reload(); err != nil {
		m.msg = "reload failed: " + err.Error()
	}
	m.msg = "AI review finished — findings loaded"
	return nil
}

// itoa avoids fmt in the hot status path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
