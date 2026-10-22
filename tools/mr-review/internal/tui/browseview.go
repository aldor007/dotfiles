// browseview.go — continuous-diff rendering (manual review mode), tuicr-style:
// ALL changed files flattened into one scrollable virtual document. File
// headers separate the files; the cursor moves through everything with
// j/k; comments, ranges, hunk marks and markers attach to any row.
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
	top := m.detailTop[tabDiff]
	if top > len(doc)-1 {
		top = max(0, len(doc)-1)
	}
	if top < 0 {
		top = 0
	}
	m.detailTop[tabDiff] = top
	end := top + h
	if end > len(doc) {
		end = len(doc)
	}

	hint := "esc findings · [/] hunk · { } file · V range · c comment · m file · / search"
	title := fmt.Sprintf("CONTINUOUS DIFF · file %d/%d · row %d/%d  %s",
		m.browseFileIndex()+1, len(m.changedFiles()), m.diffCursor+1, len(doc), hint)
	out := []string{styleSection.Render(cut(title, w))}

	selLo, selHi := m.visualRange()
	selFile := ""
	if m.visualOn && selLo < len(doc) {
		selFile = doc[selLo].file
	}
	marked := false
	for i := top; i < end; i++ {
		r := doc[i]
		var line string
		if r.header {
			line = m.renderBrowseFileHeader(r.file, w)
		} else {
			line = m.renderBrowseLine(r.file, r.dl, i, selLo, selHi, selFile)
		}
		if i == m.diffCursor {
			if plainMode() {
				line = styleCursor.Render(pad(cut(stripANSIish(line), w), w))
			} else {
				line = styleCursor.Render(pad(cut(line, w), w))
			}
			marked = true
		}
		out = append(out, cut(line, w))
	}
	if !marked { // cursor scrolled out (shouldn't happen): show position
		out = append(out, styleDim.Render(fmt.Sprintf("cursor at row %d/%d", m.diffCursor+1, len(doc))))
	}
	return out
}

// renderBrowseFileHeader renders one file header row of the continuous diff.
func (m *model) renderBrowseFileHeader(path string, w int) string {
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
	mark := " "
	if m.reviewedSet()[path] {
		mark = "✓"
	}
	fc := m.findingsPerFile()[path]
	cc := m.commentsPerFile()[path]
	meta := ""
	if fc > 0 || cc > 0 {
		meta = styleDim.Render(fmt.Sprintf("  · %d finding%s · %d comment%s",
			fc, plural(fc), cc, plural(cc)))
	}
	return cutPlainLine(fmt.Sprintf("%s %s %s%s",
		style.Render(status), plainOr(mark, styleOK.Render(mark)),
		styleSection.Render(path), meta), w)
}

// renderBrowseLine renders one diff row with gutter + marker column.
func (m *model) renderBrowseLine(path string, l DiffLine, idx, selLo, selHi int, selFile string) string {
	if l.Kind == DiffHunkHeader {
		mark := " "
		if m.reviewedHunkSet()[fmt.Sprintf("%s|%d", path, hunkStartOf(l.Text))] {
			mark = "✓"
		}
		return clipPlain(fmt.Sprintf("     %s %s", mark, l.Text), 200)
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
	if marker == "" {
		marker = " "
	}
	inSel := m.visualOn && selFile == path && idx >= selLo && idx <= selHi
	gutter := fmt.Sprintf("%4s %4s %s ", numStr(l.OldNo), numStr(l.NewNo), marker)
	col, mark := "", " "
	switch l.Kind {
	case DiffAdd:
		col, mark = green, "+"
	case DiffDel:
		col, mark = red, "-"
	}
	body := l.Text
	if !plainMode() {
		hl := HighlightedCodeLines(path, []string{l.Text}, false)
		if len(hl) == 1 {
			body = hl[0]
		}
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
