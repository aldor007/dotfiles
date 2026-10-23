// clipboard.go — contextual copy (y) via Bubble Tea v2's SetClipboard,
// which emits OSC52: it lands in the system clipboard over SSH and inside
// herdr panes without CGO. Nothing here touches GitLab — clipboard only.
package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// copyText returns the text `y` should copy in the current context plus a
// short description for the status line. Precedence: the LEFT-pane view
// wins (pressing 2/3 switches what y means), then the continuous diff.
//   - comments navigator → the selected draft/finding/remote text
//   - files tree → the selected file path
//   - continuous diff → visual selection, else the row under the cursor
//     (code text only; file-header rows carry no content)
//   - findings navigator → the finding's publish-ready comment
func (m *model) copyText() (text, desc string, ok bool) {
	switch {
	case m.leftView == viewComments:
		r := m.selectedRow()
		if r == nil {
			return "", "pick a comment first — j/k to navigate", false
		}
		switch r.kind {
		case "draft":
			return r.draft.Text, "draft comment", true
		case "finding":
			return r.finding.EffectiveComment(), "finding comment", true
		default:
			return r.text, "remote comment", true
		}

	case m.leftView == viewFiles:
		p := m.treeSelection()
		if p == "" {
			return "", "pick a file first — j/k in the Files view", false
		}
		return p, "file path", true

	case m.focus == focusDiff && m.tab == tabOverview && m.browseFile != "":
		doc := m.browseDoc()
		if len(doc) == 0 {
			return "", "empty diff", false
		}
		lo, hi := m.diffCursor, m.diffCursor
		if m.visualOn {
			lo, hi = m.visualRange()
		}
		var b strings.Builder
		n := 0
		for i := lo; i <= hi && i < len(doc); i++ {
			if doc[i].header {
				continue // file separator rows carry no content
			}
			if n > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(doc[i].dl.Text)
			n++
		}
		if n == 0 {
			return "", "no diff row under the cursor", false
		}
		if lo != hi {
			return b.String(), "selected lines", true
		}
		return b.String(), "diff line", true

	default: // findings navigator
		f := m.current()
		if f == nil {
			return "", "pick a finding first — j/k in the Findings view", false
		}
		return f.EffectiveComment(), "finding comment", true
	}
}

// runCopy executes the contextual y action: fills the clipboard (OSC52 via
// the program) and reports what was copied.
func (m *model) runCopy() tea.Cmd {
	text, desc, ok := m.copyText()
	if !ok {
		m.msg = "tip — " + desc + ". pick something first, then press y to copy."
		return nil
	}
	m.msg = fmt.Sprintf("copied %s (%d line(s))", desc, strings.Count(text, "\n")+1)
	return tea.SetClipboard(text)
}
