// commentspane.go — comment navigator rendering (moved verbatim from
// commentnav.go; the draft lifecycle logic stays in commentnav.go).
package tui

import (
	"fmt"
	"strings"

	"mr-review/internal/store"
	"mr-review/internal/tui/widget"

	"charm.land/lipgloss/v2"
)

// ---- navigator view --------------------------------------------------------

func (m *model) commentsView() []string {
	w := m.listWidth()
	h := m.bodyHeight() - 2
	var out []string
	hdr := fmt.Sprintf("COMMENTS · %d drafts · %d ready", len(m.comments), m.readyDraftCount())
	if m.searchText != "" {
		hdr += " · filter /" + m.searchText
	}
	hdr += styleDim.Render(" · c new · s stage · P publish")
	if m.focus == focusFindings && m.leftView == viewComments {
		out = append(out, styleBorderF.Render("● "+hdr))
	} else {
		out = append(out, styleDim.Render("○ "+hdr))
	}
	out = append(out, styleBorder.Render(strings.Repeat("─", max(4, w-2))))

	rows := m.commentRows()
	if len(rows) == 0 {
		msg := "no comments yet — press c on a diff line"
		if m.searchText != "" {
			msg = "no comments match /" + m.searchText
		}
		out = append(out, centered(msg, w, h-2)...)
		return m.padRows(out, w, h)
	}
	// keep the cursor visible (widget.List scroll math, legacy semantics)
	l := widget.List{Cursor: m.commentCur, Len: len(rows)}
	top, _ := l.Window(h - 2)
	for i := top; i < len(rows) && len(out) < h; i++ {
		r := rows[i]
		var st string
		switch r.kind {
		case "draft":
			st = decisionStyleOf(r.draft.State == store.CommentReady, r.draft.Publication != nil).Render(r.stateLabel())
		case "finding":
			st = styleOK.Render(r.stateLabel())
			if r.finding.Publication != nil {
				st = stylePublished.Render("published")
			}
		default:
			st = styleDim.Render(r.stateLabel())
		}
		line := fmt.Sprintf("%s %s %s",
			styleSel.Render(r.marker()),
			truncPlain(r.loc, w/3),
			truncPlain(r.text, max(8, w-lipgloss.Width(r.marker())-w/3-8)))
		line = pad(line, w-10) + st
		if i == m.commentCur {
			line = styleCursor.Render(pad(cut(line, w), w))
		}
		out = append(out, line)
	}
	return m.padRows(out, w, h)
}
