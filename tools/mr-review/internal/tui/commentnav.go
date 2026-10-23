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

	tea "charm.land/bubbletea/v2"
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

func (m *model) readyDraftCount() int {
	n := 0
	for i := range m.comments {
		if m.comments[i].State == store.CommentReady && m.comments[i].Publication == nil {
			n++
		}
	}
	return n
}

// ---- draft editor overlay ---------------------------------------------------
// The editor itself (draftEditor state, rendering, key handling, preview)
// lives in commenteditor.go. This file keeps the invocation paths: how a
// comment request becomes an anchored editor.

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
			m.msg = "no diff lines here — press j/k to move to a hunk, then try again"
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
		e.findingID = f.ID // the strip shows which finding the comment anchors at
	} else {
		m.msg = "select a diff location first (2 → files, enter; or gd)"
		return nil
	}
	return m.openDraftEditor(e)
}

func (m *model) startGeneralComment() tea.Cmd {
	return m.openDraftEditor(&draftEditor{general: true})
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
		m.msg = "switch to the diff first (gd, or 2 → file) to select a range"
		return
	}
	m.visualOn = true
	m.visualStart = m.diffCursor
	m.msg = "range selected — j/k to extend · c to comment · esc to cancel"
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
