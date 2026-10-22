// commentnav.go — unified comment navigator and the manual comment workflow.
//
// The navigator lists every comment-like item on the MR: local human drafts
// (ready or held back), approved AI findings, published items, and existing
// remote GitLab discussions. Manual comments are independent of AI findings
// and are always local until an explicit publish run.
package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"mr-review/internal/store"
	"mr-review/internal/tui/widget"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// commentRow is one navigator entry.
type commentRow struct {
	kind    string // draft | finding | remote
	loc     string
	text    string
	draft   *store.DraftComment
	finding *store.Finding
}

func (r commentRow) marker() string {
	switch r.kind {
	case "draft":
		switch {
		case r.draft.Publication != nil:
			return icons.CommentPublished
		case r.draft.State == store.CommentReady:
			return icons.CommentReady
		default:
			return icons.CommentHeld
		}
	case "finding":
		return icons.FindingMark
	default:
		return icons.RemoteMark
	}
}

func (r commentRow) stateLabel() string {
	switch r.kind {
	case "draft":
		switch {
		case r.draft.Publication != nil:
			return "published"
		case r.draft.State == store.CommentReady:
			return "ready"
		default:
			return "held"
		}
	case "finding":
		return "AI approved"
	default:
		return "remote"
	}
}

// remoteNote is a minimal existing GitLab discussion (read-only in the TUI).
type remoteNote struct {
	loc     string
	author  string
	body    string
	discID  string
	pending bool
}

// loadRemoteNotes reads discussions.json written during the review start.
func loadRemoteNotes(dir string) []remoteNote {
	b, err := os.ReadFile(dir + "/discussions.json")
	if err != nil {
		return nil
	}
	var discs []struct {
		ID    string `json:"id"`
		Notes []struct {
			Author   struct{ Username string } `json:"author"`
			Body     string                    `json:"body"`
			System   bool                      `json:"system"`
			Res      bool                      `json:"resolvable"`
			Resolved bool                      `json:"resolved"`
			Position *struct {
				NewPath string `json:"new_path"`
				NewLine *int   `json:"new_line"`
				OldLine *int   `json:"old_line"`
			} `json:"position"`
		} `json:"notes"`
	}
	if err := json.Unmarshal(b, &discs); err != nil {
		return nil
	}
	var out []remoteNote
	for _, d := range discs {
		for _, n := range d.Notes {
			if n.System || strings.TrimSpace(n.Body) == "" {
				continue
			}
			r := remoteNote{author: n.Author.Username, body: n.Body, discID: d.ID}
			if n.Position != nil && n.Position.NewPath != "" {
				line := 0
				if n.Position.NewLine != nil {
					line = *n.Position.NewLine
				} else if n.Position.OldLine != nil {
					line = *n.Position.OldLine
				}
				r.loc = fmt.Sprintf("%s:%d", n.Position.NewPath, line)
			}
			out = append(out, r)
		}
	}
	return out
}

// commentRows builds the (search-filtered) navigator rows.
func (m *model) commentRows() []commentRow {
	if !m.remoteLoaded {
		m.remoteNotes = loadRemoteNotes(m.dir)
		m.remoteLoaded = true
	}
	q := strings.ToLower(m.searchText)
	var out []commentRow
	for i := range m.comments {
		c := &m.comments[i]
		row := commentRow{kind: "draft", draft: c, text: firstLine(c.Text)}
		row.loc = c.Location()
		if row.loc == "" {
			row.loc = "(general)"
		}
		if q == "" || strings.Contains(strings.ToLower(c.Text+" "+row.loc), q) {
			out = append(out, row)
		}
	}
	for i := range m.findings {
		f := &m.findings[i]
		if f.Decision != "approved" {
			continue
		}
		row := commentRow{kind: "finding", finding: f, loc: fmt.Sprintf("%s:%d", f.File, f.Line),
			text: firstLine(f.EffectiveComment())}
		if f.Publication != nil {
			row.kind = "finding" // published state shows in the label
		}
		if q == "" || strings.Contains(strings.ToLower(f.EffectiveComment()+" "+row.loc), q) {
			out = append(out, row)
		}
	}
	for _, r := range m.remoteNotes {
		if q != "" && !strings.Contains(strings.ToLower(r.body+" "+r.loc), q) {
			continue
		}
		out = append(out, commentRow{kind: "remote", loc: r.loc,
			text: firstLine(r.author + ": " + r.body)})
	}
	return out
}

func (m *model) filteredComments() []commentRow { return m.commentRows() }

func (m *model) filteredCommentCount() int { return len(m.commentRows()) }

func (m *model) selectedRow() *commentRow {
	rows := m.commentRows()
	if m.commentCur < 0 || m.commentCur >= len(rows) {
		return nil
	}
	return &rows[m.commentCur]
}

// selectedDraft returns the draft when a local draft row is selected.
func (m *model) selectedDraft() *store.DraftComment {
	if r := m.selectedRow(); r != nil && r.kind == "draft" {
		return r.draft
	}
	return nil
}

func (m *model) hasDraftSel() bool { return m.selectedDraft() != nil }

// ---- navigator view --------------------------------------------------------

func (m *model) commentsView() []string {
	w := m.listWidth()
	h := m.bodyHeight() - 2
	var out []string
	hdr := fmt.Sprintf("COMMENTS · %d drafts · %d ready", len(m.comments), m.readyDraftCount())
	if m.searchText != "" {
		hdr += " · filter /" + m.searchText
	}
	if m.focus == focusList && m.leftView == viewComments {
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
		return m.padRows(out, h)
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
	return m.padRows(out, h)
}

func (m *model) readyDraftCount() int {
	n := 0
	for i := range m.comments {
		if m.comments[i].State == store.CommentReady && m.comments[i].Publication == nil {
			n++
		}
	}
	return n
}

// ---- draft editor overlay --------------------------------------------------

// draftEditor is the multi-line editor for manual comments.
type draftEditor struct {
	id         string // "" for a new draft
	file       string
	side       string
	line       int
	lineEnd    int
	general    bool
	published  bool // editing after publication: not allowed to change text? (allowed, re-publish dup-checked)
	ta         textarea.Model
	origText   string
	armDiscard bool // unsaved-changes protection: first Esc warns
}

func (e *draftEditor) title() string {
	if e.general {
		return "GENERAL MR COMMENT"
	}
	if e.lineEnd > e.line {
		return fmt.Sprintf("COMMENT — %s:%d-%d (%s side)", shortPath(e.file), e.line, e.lineEnd, e.side)
	}
	return fmt.Sprintf("COMMENT — %s:%d (%s side)", shortPath(e.file), e.line, e.side)
}

// startDraftComment opens the editor at the current location: the continuous
// diff cursor (or visual range), else the selected finding's line.
func (m *model) startDraftComment() tea.Cmd {
	e := &draftEditor{}
	if m.browseFile != "" {
		doc := m.browseDoc()
		if len(doc) == 0 || m.diffCursor >= len(doc) {
			m.msg = "no diff row under the cursor"
			return nil
		}
		start, end := m.diffCursor, m.diffCursor
		if m.visualOn {
			start, end = min(m.visualStart, m.diffCursor), max(m.visualStart, m.diffCursor)
		}
		// clamp non-addressable rows (file headers, hunk headers) out of the
		// selection: they carry no line numbers to comment on
		unaddressable := func(i int) bool {
			return doc[i].header || doc[i].dl.Kind == DiffHunkHeader
		}
		for start <= end && unaddressable(start) {
			start++
		}
		for end >= start && unaddressable(end) {
			end--
		}
		if start > end {
			m.msg = "selection has no diff rows — move to a diff line (j/k)"
			return nil
		}
		if doc[start].file != doc[end].file {
			m.msg = "range spans multiple files — comment them separately"
			return nil
		}
		startRow, endRow := doc[start], doc[end]
		e.file = startRow.file
		if startRow.dl.OldNo != 0 && startRow.dl.NewNo == 0 {
			e.side, e.line = "old", startRow.dl.OldNo
			e.lineEnd = endRow.dl.OldNo
		} else {
			e.side, e.line = "new", startRow.dl.NewNo
			e.lineEnd = endRow.dl.NewNo
			if e.line == 0 {
				e.side, e.line = "old", startRow.dl.OldNo
				e.lineEnd = endRow.dl.OldNo
			}
		}
		if e.line == 0 {
			m.msg = "this row has no line number on either side"
			return nil
		}
	} else if f := m.current(); f != nil {
		e.file, e.side, e.line, e.lineEnd = f.File, f.Side, f.Line, f.LineEnd
	} else {
		m.msg = "select a diff location first (2 → files, enter; or gd)"
		return nil
	}
	return m.openDraftEditor(e)
}

func (m *model) startGeneralComment() tea.Cmd {
	return m.openDraftEditor(&draftEditor{general: true})
}

func (m *model) openDraftEditor(e *draftEditor) tea.Cmd {
	ta := textarea.New()
	ta.Placeholder = "Write the comment (Markdown)…"
	ta.MaxHeight = 10
	ta.SetValue(e.origText)
	ta.Focus()
	e.ta = ta
	m.draftEdit = e
	m.cancelVisual()
	return textarea.Blink
}

func (m *model) updateDraftEditor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	e := m.draftEdit
	switch msg.String() {
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
		m.msg = "draft saved (ready to publish — see view 3)"
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
	}
	e.armDiscard = false
	var cmd tea.Cmd
	e.ta, cmd = e.ta.Update(msg)
	return m, cmd
}

// saveDraft persists the draft (insert or update) atomically. The in-memory
// list is authoritative within the session (mirrors setDecision/findings).
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
		cs = append(cs, store.NewDraftComment(id, e.file, e.side, e.line, e.lineEnd, e.general, text))
	}
	if err := store.SaveComments(m.dir, cs); err != nil {
		return err
	}
	m.comments = cs
	return nil
}

// editSelectedDraft opens the editor with the selected draft's text.
func (m *model) editSelectedDraft() tea.Cmd {
	c := m.selectedDraft()
	if c == nil {
		m.msg = "select a local draft (✦ rows are AI findings: use e in findings view)"
		return nil
	}
	e := &draftEditor{
		id: c.ID, file: c.File, side: c.Side, line: c.Line, lineEnd: c.LineEnd,
		general: c.General, published: c.Publication != nil,
		origText: c.Text,
	}
	return m.openDraftEditor(e)
}

func (m *model) confirmDeleteDraft() {
	if m.selectedDraft() == nil {
		return
	}
	m.confirmDelete = true
}

func (m *model) deleteSelectedDraft() {
	c := m.selectedDraft()
	if c == nil {
		return
	}
	id := c.ID
	var out []store.DraftComment
	for _, d := range m.comments {
		if d.ID != id {
			out = append(out, d)
		}
	}
	if err := store.SaveComments(m.dir, out); err != nil {
		m.msg = "delete failed: " + err.Error()
		return
	}
	m.comments = out
	if m.commentCur >= len(out) {
		m.commentCur = len(out) - 1
	}
	if m.commentCur < 0 {
		m.commentCur = 0
	}
	m.msg = "draft " + id + " deleted"
}

func (m *model) toggleDraftReady() {
	c := m.selectedDraft()
	if c == nil {
		return
	}
	if c.State == store.CommentReady {
		c.State = store.CommentDraft
		m.msg = c.ID + " held back (not offered for publication)"
	} else {
		c.State = store.CommentReady
		m.msg = c.ID + " ready to publish"
	}
	if err := store.SaveComments(m.dir, m.comments); err != nil {
		m.msg = "save failed: " + err.Error()
	}
}

// editDraftExternal opens the selected draft in $EDITOR (secure temp file).
func (m *model) editDraftExternal() tea.Cmd {
	c := m.selectedDraft()
	if c == nil {
		m.msg = "select a local draft first"
		return nil
	}
	tmp, err := os.CreateTemp("", "mr-review-comment-*.md")
	if err != nil {
		m.msg = err.Error()
		return nil
	}
	if _, err := tmp.WriteString(c.Text); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		m.msg = err.Error()
		return nil
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		m.msg = err.Error()
		return nil
	}
	tmp.Close()
	id, dir := c.ID, m.dir
	ed := editorBin()
	// editor binary + temp path only — no shell, no interpolation of content
	return tea.ExecProcess(exec.Command(ed, tmp.Name()), func(err error) tea.Msg {
		defer os.Remove(tmp.Name())
		if err != nil {
			return childDoneMsg{err: err, note: "editor failed (draft unchanged)"}
		}
		b, rerr := os.ReadFile(tmp.Name())
		if rerr != nil {
			return childDoneMsg{err: rerr, note: "reading editor output"}
		}
		text := strings.TrimSpace(string(b))
		if text == "" {
			return childDoneMsg{note: "editor produced empty text (draft unchanged)"}
		}
		cs, lerr := store.LoadComments(dir)
		if lerr != nil {
			return childDoneMsg{err: lerr, note: "reload drafts"}
		}
		for i := range cs {
			if cs[i].ID == id {
				cs[i].Text = text
				cs[i].UpdatedAt = store.Now()
			}
		}
		if serr := store.SaveComments(dir, cs); serr != nil {
			return childDoneMsg{err: serr, note: "save drafts"}
		}
		return childDoneMsg{note: "draft " + id + " updated from $EDITOR"}
	})
}

// publishApproveCmd runs the separate formal-approval CLI (its own typed
// confirmation); it is never part of a comment publication batch.
func (m *model) publishApproveCmd() tea.Cmd {
	bin, err := publisherBinary()
	if err != nil {
		m.msg = err.Error()
		return nil
	}
	m.busy = true
	m.busySince = time.Now()
	dir := m.dir
	return tea.Batch(spinTick(m), tea.ExecProcess(exec.Command(bin, "approve", dir), func(err error) tea.Msg {
		note := "MR approval finished"
		if err != nil {
			note = "MR approval did not complete (see output)"
		}
		return childDoneMsg{note: note}
	}))
}

// ---- visual selection ------------------------------------------------------

func (m *model) startVisual() {
	if m.browseFile == "" {
		m.msg = "range selection works in the continuous diff (gd / file tree)"
		return
	}
	m.visualOn = true
	m.visualStart = m.diffCursor
	m.msg = "range selection — j/k to extend, c to comment, esc cancels"
}

func (m *model) cancelVisual() {
	m.visualOn = false
	m.visualStart = -1
}

// visualRange returns the inclusive row range of the selection (browse mode).
func (m *model) visualRange() (int, int) {
	if !m.visualOn {
		return m.diffCursor, m.diffCursor
	}
	return min(m.visualStart, m.diffCursor), max(m.visualStart, m.diffCursor)
}

// ---- helpers ---------------------------------------------------------------
