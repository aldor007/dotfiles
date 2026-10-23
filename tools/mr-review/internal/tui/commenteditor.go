// commenteditor.go — the floating manual-comment editor (redesign §6).
//
// One bordered box composited OVER the unchanged frame (see overlay.go),
// with two regions plus a hint footer:
//
//	┌ context strip: file · line[:end] · side | FILE comment | MR-level ─┐
//	│ ────────────────────────────────────────────────────────────────── │
//	│ textarea (min 8 rows, ≤40% of the terminal)                        │
//	│ ────────────────────────────────────────────────────────────────── │
//	└ hint: ctrl+s save · esc cancel · ctrl+r preview ───────────────────┘
//
// Preview key: Ctrl+R. Ctrl+P is NOT usable — bubbles v2's textarea binds
// LinePrevious (cursor up) to ctrl+p, and that binding must keep working
// inside the editor. Ctrl+R is unbound in the textarea keymap and in the
// action registry, so it is free for the Markdown toggle.
//
// Input safety: while the editor is open curMode() is modeDraftEdit, so the
// global registry dispatch never runs; every printable key goes to the
// textarea (or is ignored in preview mode). Esc is guarded by armDiscard.
package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"mr-review/internal/store"
)

// previewToggleKey toggles Markdown preview. Chosen over Ctrl+P: the
// bubbles v2 textarea keymap binds ctrl+p to LinePrevious (cursor up).
const previewToggleKey = "ctrl+r"

// draftEditor is the state of the floating manual comment editor. The zero
// value plus a textarea is valid (tests construct it directly).
type draftEditor struct {
	id        string // "" for a new draft
	file      string
	side      string
	line      int
	lineEnd   int
	general   bool
	published bool   // editing after publication (re-publish is dup-checked)
	findingID string // non-empty when anchored at a finding: shown in the strip

	ta         textarea.Model
	origText   string
	armDiscard bool // unsaved-changes protection: first Esc warns

	preview bool // Markdown preview (Glamour) replaces the textarea
	prevTop int  // preview scroll offset (rendered lines)
}

// editorRows sizes the textarea: at least 8 rows, at most 40% of the
// terminal height (capped at 14 so generous never means dominating), and
// capped further so the comment anchor stays visible above the centered
// box (the frame needs its top content row clear — see showEditorAnchor).
func editorRows(termH int) int {
	rows := termH * 2 / 5
	if rows > 14 {
		rows = 14
	}
	if rows < 8 {
		rows = 8
	}
	// frame: header(2) + footer(2); box chrome: strip + 2 rules + hint +
	// borders(2) = 7 rows over the textarea; anchor needs content row 0
	// above the box top (centered compositeOverlay placement).
	if room := termH - 19; rows > room {
		rows = room
	}
	if rows < 5 {
		rows = 5 // very short terminals degrade below the 8-row floor
	}
	return rows
}

// editorBoxW is the outer box width (the bordered box total cell width).
func editorBoxW(w int) int {
	return max(20, min(w-6, 92))
}

// editorInnerW is the inner content width of the editor box.
func editorInnerW(w int) int { return editorBoxW(w) - 2 }

// contextLine renders the top strip: the edit/preview mode, where the
// comment is anchored (file · line[:end] · side, FILE comment, MR-level,
// finding ID) and the unsaved warning.
func (e *draftEditor) contextLine(w int) string {
	mode := styleSel.Render("EDIT")
	if e.preview {
		mode = styleOK.Render("PREVIEW")
	}
	// reserve room for the mode label (and the warn chip) so they survive
	// the final cut; the file path shrinks first via smartPath
	budget := max(16, w-cellWidth(mode)-2)
	warn := ""
	if e.armDiscard {
		warn = styleWarn.Render("UNSAVED — esc discards")
		budget -= cellWidth(warn) + 2
	}
	var target string
	switch {
	case e.general:
		target = "MR-level comment · not attached to a diff line"
	case e.file == "":
		target = "MR-level comment"
	case e.line == 0:
		target = "FILE comment · " + smartPath(e.file, max(12, budget-16))
	default:
		loc := fmt.Sprintf("%s · %d", smartPath(e.file, max(12, budget-14)), e.line)
		if e.lineEnd > e.line {
			loc += fmt.Sprintf("-%d", e.lineEnd)
		}
		target = loc + " · " + e.side + " side"
	}
	if e.findingID != "" {
		target = e.findingID + " · " + target
	}
	line := mode + "  " + styleHeader.Render(target)
	if warn != "" {
		line += "  " + warn
	}
	return cut(line, w)
}

// previewLines renders the comment Markdown (Glamour) at the preview
// width. Empty comments get an explicit note instead of a blank pane.
func (m *model) previewLines(e *draftEditor, w int) []string {
	if strings.TrimSpace(e.ta.Value()) == "" {
		return []string{styleDim.Render("(empty comment — nothing to preview)")}
	}
	out := renderMarkdown(e.ta.Value(), w)
	if strings.TrimSpace(stripANSIish(out)) == "" {
		return []string{styleDim.Render("(empty comment — nothing to preview)")}
	}
	return strings.Split(out, "\n")
}

// draftEditorView renders the floating two-region comment editor. The
// underlying frame keeps rendering underneath (View() composites this box
// over it); openDraftEditor scrolled the diff so the anchor line renders
// above the box. The footer hint rides in overlayBoxLines' hint slot.
func (m *model) draftEditorView(w, h int) []string {
	e := m.draftEdit
	boxW := editorBoxW(w)
	inner := editorInnerW(w)
	rows := editorRows(m.height)

	var b strings.Builder
	b.WriteString(e.contextLine(inner) + "\n")
	b.WriteString(styleBorder.Render(strings.Repeat("─", inner)) + "\n")
	if e.preview {
		lines := m.previewLines(e, inner-2)
		top := min(max(0, e.prevTop), max(0, len(lines)-rows))
		end := min(len(lines), top+rows)
		for _, l := range lines[top:end] {
			b.WriteString(cut(l, inner) + "\n")
		}
	} else {
		taLines := strings.Split(e.ta.View(), "\n")
		for len(taLines) < rows {
			taLines = append(taLines, "")
		}
		for _, l := range taLines {
			b.WriteString(cut(l, inner) + "\n")
		}
	}
	b.WriteString(styleBorder.Render(strings.Repeat("─", inner)) + "\n")

	hints := "ctrl+s save draft (local only) · esc cancel · ctrl+r markdown preview"
	if e.preview {
		hints = "ctrl+r back to edit · j/k scroll preview · ctrl+s save · esc cancel"
	}
	return overlayBoxLines(b.String(), boxW, rows+3, hints)
}

// openDraftEditor sizes the textarea and opens the editor over the diff.
func (m *model) openDraftEditor(e *draftEditor) tea.Cmd {
	ta := textarea.New()
	ta.Placeholder = "Write the comment (Markdown)…"
	rows := editorRows(m.height)
	ta.SetHeight(rows)
	ta.MaxHeight = rows // fixed viewport height; content scrolls inside
	if m.width > 0 {
		ta.SetWidth(editorInnerW(m.width) - 2)
	}
	ta.SetValue(e.origText)
	ta.Focus()
	e.ta = ta
	e.preview = false
	e.prevTop = 0
	m.draftEdit = e
	m.cancelVisual()
	m.showEditorAnchor(e)
	return textarea.Blink
}

// showEditorAnchor scrolls the diff viewport so the comment's anchor line
// renders as the FIRST row of the diff pane. The editor box floats over
// the center of the frame, so the anchor (plus the lines under it) must
// sit above the box top to stay visible while writing.
func (m *model) showEditorAnchor(e *draftEditor) {
	if e.general || e.file == "" || e.line == 0 {
		return
	}
	if m.browseFile != "" {
		for i, r := range m.browseDoc() {
			if r.header || r.file != e.file {
				continue
			}
			no := r.dl.NewNo
			if e.side == "old" {
				no = r.dl.OldNo
			}
			if no == e.line {
				m.detailTop[tabOverview] = max(0, i)
				return
			}
		}
		return
	}
	// finding-window diff mode: only adjust when the anchor is the finding
	// currently shown (a draft edited from the navigator must not yank the
	// viewport to an unrelated file)
	if f := m.current(); f != nil && f.File == e.file {
		m.ensureDiff()
		fd := m.diffParsed[e.file]
		if fd == nil {
			return
		}
		_, hl := DiffWindow(fd, e.side, e.line, e.lineEnd, m.ctxLines)
		m.detailTop[tabOverview] = max(0, hl-1)
	}
}

// updateDraftEditor routes keys while the comment editor is open
// (modeDraftEdit): the registry dispatch never runs, so no global action
// can fire — printable keys reach the textarea, preview-mode keys scroll
// the preview, everything else is swallowed.
func (m *model) updateDraftEditor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	e := m.draftEdit
	k := msg.String()
	switch k {
	case "ctrl+s", "ctrl+enter":
		text := strings.TrimSpace(e.ta.Value())
		if text == "" {
			m.msg = "comment is empty — nothing saved"
			return m, nil
		}
		if err := m.saveDraft(e, text); err != nil {
			m.msg = "save failed: " + err.Error()
			return m, nil
		}
		m.draftEdit = nil
		m.msg = "draft saved (held — mark it ready in the comment navigator, view 3)"
		return m, m.bgPendingReload()
	case "esc":
		if strings.TrimSpace(e.ta.Value()) != strings.TrimSpace(e.origText) && !e.armDiscard {
			e.armDiscard = true
			m.msg = "unsaved changes — esc again discards, ctrl+s saves"
			return m, nil
		}
		m.draftEdit = nil
		m.msg = "comment discarded"
		return m, m.bgPendingReload()
	case previewToggleKey:
		e.preview = !e.preview
		e.prevTop = 0
		if e.preview {
			e.ta.Blur() // j/k scroll the preview instead of editing
			return m, nil
		}
		return m, e.ta.Focus()
	}
	if e.preview {
		rows := editorRows(m.height)
		switch k {
		case "j", "down":
			e.prevTop++
		case "k", "up":
			if e.prevTop > 0 {
				e.prevTop--
			}
		case "ctrl+d":
			e.prevTop += rows / 2
		case "ctrl+u":
			e.prevTop -= rows / 2
		default:
			return m, nil // printable keys do nothing in preview (no loss)
		}
		lines := len(m.previewLines(e, editorInnerW(m.width)-2))
		e.prevTop = min(max(0, e.prevTop), max(0, lines-rows))
		return m, nil
	}
	e.armDiscard = false
	var cmd tea.Cmd
	e.ta, cmd = e.ta.Update(msg)
	return m, cmd
}

// saveDraft persists the draft (insert or update) atomically. New drafts
// start HELD: saving must never mark a comment ready-to-publish
// implicitly — toggling ready in the navigator (view 3) is the explicit
// step. The in-memory list is authoritative within the session (mirrors
// setDecision/findings).
func (m *model) saveDraft(e *draftEditor, text string) error {
	cs := m.comments
	found := false
	for i := range cs {
		if cs[i].ID == e.id && e.id != "" {
			cs[i].Text = text
			cs[i].UpdatedAt = store.Now()
			found = true
		}
	}
	if !found {
		id := store.NextCommentID(cs)
		c := store.NewDraftComment(id, e.file, e.side, e.line, e.lineEnd, e.general, text)
		c.State = store.CommentDraft // held until explicitly marked ready
		cs = append(cs, c)
	}
	if err := store.SaveComments(m.dir, cs); err != nil {
		return err
	}
	m.comments = cs
	return nil
}
