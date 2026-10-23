// diffmode.go — side-by-side (old|new) rendering of the continuous diff
// document, plus shared cell-exact helpers used by both diff modes.
//
// The continuous document (browseDoc) stays the single source of truth:
// diffCursor keeps indexing its unified rows, so navigation, hunk/file jumps,
// search and comment anchoring are mode-independent. Side-by-side is a pure
// VIEW transform: unified rows are paired (del-run ↔ add-run, context with
// itself) into sbsRow values that remember which document rows they cover,
// so the cursor keeps landing on the correct SIDE (old for deletions,
// new for additions/context) and ranges/comments stay anchored.
package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// minSBSWidth is the minimum diff-pane width for side-by-side mode; below it
// the renderer falls back to unified with a status message. Two columns stay
// readable down to ~44 cells per side — the wide layout reaches 100 diff-pane
// cells at a 200-col terminal (files 44 + findings 48 + chrome 8).
const minSBSWidth = 100 - 12

// toggleDiffSideBySide flips the session-scoped side-by-side preference.
// Registry entry (added by the integrator): "diff.side-by-side".
func (m *model) toggleDiffSideBySide() {
	if m.browseFile == "" {
		m.msg = "side-by-side applies to the continuous diff (gd first)"
		return
	}
	if !m.diffSideBySide && m.diffPaneWidth() < minSBSWidth {
		m.msg = fmt.Sprintf("side-by-side needs width ≥ %d (diff pane is %d) — staying unified", minSBSWidth, m.diffPaneWidth())
		return
	}
	m.diffSideBySide = !m.diffSideBySide
	if m.diffSideBySide {
		m.msg = "side-by-side diff (old | new)"
	} else {
		m.msg = "unified diff"
	}
}

// diffPaneWidth is the inner width available to the center diff pane.
func (m *model) diffPaneWidth() int {
	L := m.layout()
	if L.mode == layoutNarrow {
		return m.width - 2
	}
	return L.diffW
}

// sideBySideActive reports whether the current frame renders old|new columns
// (preference on AND the pane wide enough — otherwise unified fallback).
func (m *model) sideBySideActive() bool {
	return m.diffSideBySide && m.diffPaneWidth() >= minSBSWidth
}

// sbsRow is one rendered row of the side-by-side transform. Exactly one of
// header/hunk/paired applies; docLo..docHi are the covered browseDoc indexes
// (inclusive), used to keep cursor/range mapping mode-independent.
type sbsRow struct {
	file         string
	header       bool // file header row (rendered full width)
	hunk         *browseRow
	old, new     *browseRow // paired halves (either may be nil)
	docLo, docHi int
}

// sbsAnchoredSide returns "old" or "new": the side the document row at idx
// belongs to. Deletions anchor old; additions and context anchor new
// (mirroring renderBrowseLine / numberFor semantics).
func sbsAnchoredSide(r browseRow) string {
	if !r.header && r.dl.NewNo == 0 {
		return "old"
	}
	return "new"
}

// buildSBSRows transforms a slice of the continuous document into side-by-side
// rows. The del-run/add-run pairing needs the whole run, so runs that start
// above `lo` are completed by scanning back to their first row.
func buildSBSRows(doc []browseRow, lo, hi int) []sbsRow {
	if lo < 0 {
		lo = 0
	}
	if hi >= len(doc) {
		hi = len(doc) - 1
	}
	if lo > hi {
		return nil
	}
	// back up to the start of the add/del run containing lo so pairing is
	// correct even when the run began above the viewport.
	start := lo
	for start > 0 && !doc[start].header && doc[start].dl.Kind != DiffHunkHeader &&
		(doc[start-1].dl.Kind == DiffDel || doc[start-1].dl.Kind == DiffAdd) &&
		!doc[start-1].header && doc[start-1].dl.Kind != DiffHunkHeader {
		start--
	}
	out := make([]sbsRow, 0, hi-lo+1)
	i := start
	for i <= hi {
		r := doc[i]
		switch {
		case r.header:
			out = append(out, sbsRow{file: r.file, header: true, docLo: i, docHi: i})
			i++
		case r.dl.Kind == DiffHunkHeader:
			out = append(out, sbsRow{file: r.file, hunk: &doc[i], docLo: i, docHi: i})
			i++
		default:
			// one change block: maximal del run followed by maximal add run
			j := i
			var dels, adds []int
			for j < len(doc) && !doc[j].header && doc[j].dl.Kind == DiffDel {
				dels = append(dels, j)
				j++
			}
			for j < len(doc) && !doc[j].header && doc[j].dl.Kind == DiffAdd {
				adds = append(adds, j)
				j++
			}
			if len(dels) == 0 && len(adds) == 0 { // context row: pairs with itself
				out = append(out, sbsRow{file: r.file, old: &doc[i], new: &doc[i], docLo: i, docHi: i})
				i++
				break
			}
			n := min(len(dels), len(adds))
			for k := 0; k < n; k++ {
				out = append(out, sbsRow{file: r.file,
					old:   &doc[dels[k]],
					new:   &doc[adds[k]],
					docLo: min(dels[k], adds[k]), docHi: max(dels[k], adds[k])})
			}
			for _, d := range dels[n:] { // unpaired deletions: old only
				out = append(out, sbsRow{file: r.file, old: &doc[d], docLo: d, docHi: d})
			}
			for _, a := range adds[n:] { // unpaired additions: new only
				out = append(out, sbsRow{file: r.file, new: &doc[a], docLo: a, docHi: a})
			}
			i = j
		}
	}
	return out
}

// renderBrowseSBS renders the side-by-side window for doc rows [top,end).
// Cursor highlight lands on the anchored side only; visual-range rows paint
// the full row (both columns) with the selection style.
func (m *model) renderBrowseSBS(w int, doc []browseRow, top, end, selLo, selHi int, selFile string) []string {
	rows := buildSBSRows(doc, top, end)
	gw := m.gutterWidth(doc)
	sideW := (w - 1) / 2 // 1 column for the center divider
	if sideW < 12 {
		sideW = 12
	}
	divider := styleDim.Render("│")
	out := make([]string, 0, len(rows))
	for _, s := range rows {
		switch {
		case s.header:
			out = append(out, m.renderBrowseFileHeader(s.file, w))
		case s.hunk != nil:
			out = append(out, m.renderBrowseHunkHeader(s.hunk.file, s.hunk.dl.Text, w))
		default:
			inSel := m.visualOn && selFile == s.file && s.docLo >= selLo && s.docHi <= selHi
			cursorRow := m.diffCursor >= s.docLo && m.diffCursor <= s.docHi
			anchor := ""
			if cursorRow {
				anchor = sbsAnchoredSide(doc[m.diffCursor])
			}
			left := m.renderSBSSide(s.old, s.file, gw, sideW, "old", anchor, inSel)
			right := m.renderSBSSide(s.new, s.file, gw, sideW, "new", anchor, inSel)
			// cursor wash on the anchored side (the accent bar marks the row)
			if anchor == "old" {
				left = styleCursor.Render(left)
			} else if anchor == "new" {
				right = styleCursor.Render(right)
			}
			line := left + divider + right
			if inSel {
				line = styleSel.Render(line)
			}
			out = append(out, cut(line, w))
		}
	}
	return out
}

// renderSBSSide renders one column (old or new) of a side-by-side row:
// cursor bar | line number | diff sign | marker | code. r may be nil
// (blank half). The diff sign and comment/findings markers keep their
// correct side independently.
func (m *model) renderSBSSide(r *browseRow, path string, gw, sideW int, side, anchor string, inSel bool) string {
	bar := " "
	num := strings.Repeat(" ", gw)
	sign := " "
	mark := " "
	code := ""
	if r != nil {
		l := r.dl
		no := l.OldNo
		if side == "new" {
			no = l.NewNo
		}
		num = numStrW(no, gw)
		switch {
		case side == "new" && l.Kind == DiffAdd:
			sign = "+"
		case side == "old" && l.Kind == DiffDel:
			sign = "-"
		}
		if no > 0 {
			if mk := m.markerForLine(path, side, no); mk != "" {
				mark = mk
			}
		}
		plain := expandTabs(l.Text)
		code = plain
		if !plainMode() {
			if hl := HighlightedCodeLines(path, []string{plain}, false); len(hl) == 1 {
				code = hl[0]
			}
		}
		if m.diffHOff > 0 {
			code = skipCells(code, m.diffHOff)
		}
		// search match highlight (cell-exact inverse wrap)
		if m.searchText != "" {
			code = highlightMatches(code, plain, m.searchText)
		}
	}
	if anchor == side {
		bar = diffCursorBar()
	}
	fixed := bar + " " + num + " " + sign + mark + " "
	codeW := sideW - cellWidth(fixed)
	if codeW < 0 {
		codeW = 0
	}
	return pad(cut(fixed+code, sideW), sideW)
}

// gutterWidth returns the fixed line-number column width that keeps the
// gutter aligned for any line-number magnitude present in the document
// (%4s-style, widened up to 5+ digits).
// gutterWidth returns the widest line-number column the document needs.
// Cached per document version: an O(doc) scan with per-row Sprintf on every
// frame is measurable on 100k-line diffs.
func (m *model) gutterWidth(doc []browseRow) int {
	if m.gutterWVer == m.diffVersion && m.gutterWCache > 0 {
		return m.gutterWCache
	}
	w := 4
	for i := range doc {
		r := &doc[i]
		if r.header || r.dl.Kind == DiffHunkHeader {
			continue
		}
		for _, n := range [2]int{r.dl.OldNo, r.dl.NewNo} {
			if d := digits(n); d > w {
				w = d
			}
		}
	}
	m.gutterWVer, m.gutterWCache = m.diffVersion, w
	return w
}

// digits counts decimal digits without allocating (vs fmt.Sprintf).
func digits(n int) int {
	if n <= 0 {
		return 1
	}
	d := 0
	for n > 0 {
		d++
		n /= 10
	}
	return d
}

// numStrW formats a line number right-aligned in w cells ("    " for 0).
func numStrW(n, w int) string {
	if n == 0 {
		return strings.Repeat(" ", w)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) < w {
		s = strings.Repeat(" ", w-len(s)) + s
	}
	return s
}

// diffCursorBar is the accent bar shown in the cursor row's sign column
// (distinct from the reverse-video cursor wash and from range selection).
func diffCursorBar() string {
	if plainMode() {
		return "▌"
	}
	return styleSel.Render("▌")
}

// highlightMatches wraps every case-insensitive occurrence of q in the PLAIN
// text with inverse-video toggles, re-applying the result to the STYLED text.
// Cell-exact: ANSI sequences occupy no cells and are passed through untouched
// (uses \x1b[7m / \x1b[27m so the underlying SGR state survives).
func highlightMatches(styled, plain, q string) string {
	if q == "" || plain == "" {
		return styled
	}
	lp := strings.ToLower(plain)
	lq := strings.ToLower(q)
	var ranges [][2]int
	for i := 0; ; {
		j := strings.Index(lp[i:], lq)
		if j < 0 {
			break
		}
		start := i + j
		end := start + len(lq) // exclusive, in RUNE-index space of plain
		ranges = append(ranges, [2]int{start, end})
		i = end
	}
	if len(ranges) == 0 {
		return styled
	}
	// Walk both strings in lockstep: plain runes ↔ styled runes, where styled
	// may carry ANSI sequences between runes.
	var b strings.Builder
	ri := 0 // rune index into plain
	rangeIdx := 0
	i := 0
	for i < len(styled) {
		// pass ANSI sequences through
		if styled[i] == 0x1b {
			j := i + 1
			if j < len(styled) && styled[j] == '[' {
				j++ // consume the '[' — the scan below finds the final byte
				for j < len(styled) && !(styled[j] >= 0x40 && styled[j] <= 0x7e) {
					j++
				}
				if j < len(styled) {
					j++
				}
			} else {
				j = i + 1
			}
			b.WriteString(styled[i:j])
			i = j
			continue
		}
		if rangeIdx < len(ranges) && ri == ranges[rangeIdx][0] {
			b.WriteString("\x1b[7m")
		}
		_, size := decodeRuneAt(styled[i:])
		b.WriteString(styled[i : i+size])
		if rangeIdx < len(ranges) && ri < ranges[rangeIdx][1] {
			ri++
			if ri >= ranges[rangeIdx][1] {
				b.WriteString("\x1b[27m")
				rangeIdx++
			}
		} else {
			ri++
		}
		i += size
	}
	return b.String()
}

func decodeRuneAt(s string) (rune, int) {
	r, size := utf8.DecodeRuneInString(s)
	return r, size
}
