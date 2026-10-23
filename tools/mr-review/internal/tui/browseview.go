// browseview.go — continuous-diff rendering (manual review mode), tuicr-style:
// ALL changed files flattened into one scrollable virtual document. File
// headers separate the files; the cursor moves through everything with
// j/k; comments, ranges, hunk marks and markers attach to any row.
// Side-by-side (old|new) rendering lives in diffmode.go — a pure view
// transform of this document.
package tui

import (
	"fmt"
	"sort"

	"mr-review/internal/store"

	"charm.land/lipgloss/v2"
)

// browseRow is one row of the continuous all-MR diff document.
type browseRow struct {
	file   string // repo-relative path the row belongs to
	dl     DiffLine
	header bool // file header row (not diff content)
}

// browseDoc flattens every changed file into the continuous document:
// one header row per file followed by its diff rows. Cached; invalidated
// whenever the parsed diff changes (diffVersion bumps in ensureDiff).
func (m *model) browseDoc() []browseRow {
	m.ensureDiff()
	if m.browseDocCache != nil && m.browseDocVer == m.diffVersion {
		return m.browseDocCache
	}
	paths := make([]string, 0, len(m.diffParsed))
	for p := range m.diffParsed {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	doc := make([]browseRow, 0, len(paths)*8)
	for _, p := range paths {
		doc = append(doc, browseRow{file: p, header: true})
		fd := m.diffParsed[p]
		for _, l := range fd.Lines {
			doc = append(doc, browseRow{file: p, dl: l})
		}
	}
	m.browseDocCache = doc
	m.browseDocVer = m.diffVersion
	return doc
}

// invalidateBrowseDoc drops the cached continuous document.
func (m *model) invalidateBrowseDoc() {
	m.browseDocCache = nil
}

// syncBrowseFile keeps browseFile pointing at the file the cursor sits in
// (review toggles, comment positioning and tests all key off it).
func (m *model) syncBrowseFile() {
	doc := m.browseDoc()
	if m.diffCursor < 0 || m.diffCursor >= len(doc) {
		return
	}
	m.browseFile = doc[m.diffCursor].file
}

// browseFileIndex returns the 0-based position of the cursor's file among
// the changed files (for the header readout).
func (m *model) browseFileIndex() int {
	files := m.changedFiles()
	for i, f := range files {
		if f == m.browseFile {
			return i
		}
	}
	return 0
}

// markerForLine returns the inline diff marker for a (side, line) location,
// or "" when nothing is attached there. Multiple items collapse into a count.
func (m *model) markerForLine(file, side string, line int) string {
	if line == 0 {
		return ""
	}
	ready, held, pub, finding := 0, 0, 0, 0
	for i := range m.comments {
		c := &m.comments[i]
		if c.General || c.File != file || c.Side != side {
			continue
		}
		end := c.LineEnd
		if end < c.Line {
			end = c.Line
		}
		if line < c.Line || line > end {
			continue
		}
		switch {
		case c.Publication != nil:
			pub++
		case c.State == store.CommentReady:
			ready++
		default:
			held++
		}
	}
	for i := range m.findings {
		f := &m.findings[i]
		if f.File != file || f.Side != side {
			continue
		}
		end := f.LineEnd
		if end < f.Line {
			end = f.Line
		}
		if line < f.Line || line > end {
			continue
		}
		if f.Publication != nil || f.Decision == "approved" {
			finding++
		}
	}
	switch {
	case ready+held+pub+finding == 0:
		return ""
	case pub > 0:
		return "✓"
	case ready+held+finding > 1:
		return fmt.Sprintf("%d", ready+held+pub+finding)
	case ready > 0:
		return "●"
	case held > 0:
		return "◌"
	default:
		return "✦"
	}
}

// browseDiffContent renders the continuous all-MR diff window.
func (m *model) browseDiffContent(w int) []string {
	doc := m.browseDoc()
	if len(doc) == 0 {
		return []string{styleSection.Render("DIFF — no changed files in this diff")}
	}
	h := m.browseViewportH()
	top := m.detailTop[tabOverview]
	if top > len(doc)-1 {
		top = max(0, len(doc)-1)
	}
	if top < 0 {
		top = 0
	}
	m.detailTop[tabOverview] = top
	end := top + h
	if end > len(doc) {
		end = len(doc)
	}

	out := []string{styleSection.Render(cut(m.browseTitle(w), w))}

	selLo, selHi := m.visualRange()
	selFile := ""
	if m.visualOn && selLo < len(doc) {
		selFile = doc[selLo].file
	}
	marked := false
	var body []string
	if m.sideBySideActive() {
		body = m.renderBrowseSBS(w, doc, top, end, selLo, selHi, selFile)
	} else {
		gw := m.gutterWidth(doc)
		for i := top; i < end; i++ {
			r := doc[i]
			var line string
			if r.header {
				line = m.renderBrowseFileHeader(r.file, w)
			} else {
				line = m.renderBrowseLine(r.file, r.dl, i, selLo, selHi, selFile, gw)
			}
			if i == m.diffCursor {
				if plainMode() {
					line = styleCursor.Render(pad(cut(stripANSIish(line), w), w))
				} else {
					line = styleCursor.Render(pad(cut(line, w), w))
				}
				marked = true
			}
			body = append(body, cut(line, w))
		}
	}
	// In side-by-side the cursor is marked inside the anchored half (accent
	// bar + wash in renderBrowseSBS/renderSBSSide); every doc row in the
	// window is covered by exactly one sbs row, so being in-window suffices.
	if m.sideBySideActive() && m.diffCursor >= top && m.diffCursor < end {
		marked = true
	}
	out = append(out, body...)
	if !marked { // cursor scrolled out (shouldn't happen): show position
		out = append(out, styleDim.Render(fmt.Sprintf("cursor at row %d/%d", m.diffCursor+1, len(doc))))
	}
	return out
}

// browseTitle builds the status line above the continuous diff: position,
// mode chip, active range, search state and horizontal scroll indicator.
func (m *model) browseTitle(w int) string {
	doc := m.browseDoc()
	title := fmt.Sprintf("CONTINUOUS DIFF · file %d/%d · row %d/%d",
		m.browseFileIndex()+1, len(m.changedFiles()), m.diffCursor+1, len(doc))
	if m.sideBySideActive() {
		title += styleDim.Render(" · SBS")
	}
	if m.visualOn {
		lo, hi := m.visualRange()
		title += fmt.Sprintf(" · V rows %d–%d", lo+1, hi+1)
	}
	if m.searchText != "" {
		title += fmt.Sprintf(" · /%s · %d matches", m.searchText, m.searchMatches())
	}
	if m.diffHOff > 0 {
		title += styleDim.Render(fmt.Sprintf(" · → +%d", m.diffHOff))
	}
	hint := "esc findings · [/] hunk · { } file · V range · c comment · m file · / search"
	return cutPlainLine(title+"  "+styleDim.Render(hint), w)
}

// renderBrowseFileHeader renders one file header row of the continuous diff:
// `── path ── M · +a −d · reviewed · n findings · m comments`.
// Binary/empty sections and renames are called out in the meta segment.
func (m *model) renderBrowseFileHeader(path string, w int) string {
	fd := m.diffParsed[path]
	status := string(m.fileStatus(path))
	style := styleDim
	switch status {
	case "A":
		style = lipglossGreen()
	case "D":
		style = lipglossRed()
	case "M":
		style = lipglossYellow()
	}
	// display path: renames keep old → new when the parser provides it
	disp := path
	if fd != nil && fd.Rename && fd.OldPath != "" {
		disp = fd.OldPath + " → " + path
	}
	head := styleSection.Render("── " + disp + " ──")

	adds, dels := 0, 0
	if fd != nil {
		for _, l := range fd.Lines {
			switch l.Kind {
			case DiffAdd:
				adds++
			case DiffDel:
				dels++
			}
		}
	}
	meta := style.Render(status) +
		" · " + lipglossGreen().Render(fmt.Sprintf("+%d", adds)) +
		" " + lipglossRed().Render(fmt.Sprintf("−%d", dels))
	if fd != nil && fd.Binary {
		meta += styleDim.Render(" · binary file — no text diff")
	} else if fd != nil && len(fd.Lines) == 0 {
		meta += styleDim.Render(" · no text changes")
	}
	if m.reviewedSet()[path] {
		meta += " · " + plainOr("reviewed", styleOK.Render("✓ reviewed"))
	}
	if fc := m.findingsPerFile()[path]; fc > 0 {
		meta += styleDim.Render(fmt.Sprintf(" · %d finding%s", fc, plural(fc)))
	}
	if cc := m.commentsPerFile()[path]; cc > 0 {
		meta += styleDim.Render(fmt.Sprintf(" · %d comment%s", cc, plural(cc)))
	}
	return cutPlainLine(head+" "+meta, w)
}

// renderBrowseHunkHeader renders an @@ header row: reviewed mark + dim text.
func (m *model) renderBrowseHunkHeader(path, text string, w int) string {
	mark := " "
	if m.reviewedHunkSet()[fmt.Sprintf("%s|%d", path, hunkStartOf(text))] {
		mark = "✓"
	}
	return clipPlain("     "+mark+" "+styleDim.Render(text), w)
}

// renderBrowseLine renders one unified diff row:
// cursor bar | old no | new no | marker | sign | code.
func (m *model) renderBrowseLine(path string, l DiffLine, idx, selLo, selHi int, selFile string, gw int) string {
	if l.Kind == DiffHunkHeader {
		return m.renderBrowseHunkHeader(path, l.Text, 200)
	}
	side := "new"
	if l.NewNo == 0 {
		side = "old"
	}
	lineNo := l.NewNo
	if side == "old" {
		lineNo = l.OldNo
	}
	marker := m.markerForLine(path, side, lineNo)
	if marker == "" && l.NewNo != 0 && l.OldNo != 0 {
		// context rows exist on both sides: an old-side comment on a context
		// line must still surface its marker (anchoring is per side).
		marker = m.markerForLine(path, "old", l.OldNo)
	}
	if marker == "" {
		marker = " "
	}
	inSel := m.visualOn && selFile == path && idx >= selLo && idx <= selHi
	bar := " "
	if idx == m.diffCursor {
		bar = diffCursorBar()
	}
	gutter := bar + " " + numStrW(l.OldNo, gw) + " " + numStrW(l.NewNo, gw) + " " + marker + " "
	col, mark := "", " "
	switch l.Kind {
	case DiffAdd:
		col, mark = green, "+"
	case DiffDel:
		col, mark = red, "-"
	}
	plain := expandTabs(l.Text)
	body := plain
	if !plainMode() {
		if hl := HighlightedCodeLines(path, []string{plain}, false); len(hl) == 1 {
			body = hl[0]
		}
	}
	if m.diffHOff > 0 { // horizontal scroll: gutter stays, code scrolls
		body = skipCells(body, m.diffHOff)
	}
	if m.searchText != "" {
		body = highlightMatches(body, plain, m.searchText)
	}
	line := gutter + col + mark + " " + body + reset
	if inSel {
		line = styleSel.Render(line)
	}
	return line
}

// cutPlainLine ANSI-aware truncation to w cells.
func cutPlainLine(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return clipPlain(s, w)
}

// hunkStartOf extracts the new-start line from an @@ header (for hunk keys).
func hunkStartOf(header string) int {
	_, n := parseHunkHeader(header)
	return n
}
