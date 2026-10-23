package tui

import (
	"fmt"
	"regexp"
	"strings"

	"mr-review/internal/store"
	"mr-review/internal/tui/widget"

	"charm.land/lipgloss/v2"
)

// ---- segmented tab bar ------------------------------------------------------

var tabShortNames = []string{"OVER", "COMM"}

// tabBarView renders the boxed, segmented tab bar (3 rows) for the detail
// pane via widget.RenderTabBar. The bar always spans exactly w cells: the
// last segment stretches to fill the remaining width, so the bar connects
// to the pane's right edge and its bottom border doubles as the separator
// above the content.
func (m *model) tabBarView(w int) []string {
	names := tabNames
	if m.narrow {
		names = tabShortNames
	}
	// Chunk 2: only 2 tabs now; they always fit at any sane width.
	_ = tabShortNames
	return widget.RenderTabBar(names, m.tab, m.focus == focusDiff, plainMode(), w,
		widget.TabBarStyles{
			Border: styleBorder, BorderFocus: styleBorderF, Dim: styleDim,
			Active: styleTabActive, Cursor: styleCursor,
		})
}

// emptyStateContent renders a productive main-panel body when no finding is
// selected. It picks the first changed file's diff (real content, not a
// placeholder) and adds a contextual banner explaining what's happening.
func (m *model) emptyStateContent(w int) []string {
	files := m.changedFiles()
	out := []string{}

	// Banner (2-3 lines) explaining the situation
	var banner []string
	switch {
	case m.bgRunning() && len(m.findings) == 0:
		banner = []string{
			styleSection.Render("AI is analyzing this merge request."),
			styleDim.Render("Browse the changed files while findings are generated."),
		}
	case len(m.findings) == 0 && !m.bgRunning():
		banner = []string{
			styleSection.Render("No findings detected."),
			styleDim.Render("Review the changed files below — press 2 for the file tree."),
		}
	case m.current() == nil && len(m.findings) > 0:
		banner = []string{
			styleSection.Render("Pick a finding to inspect."),
			styleDim.Render("j/k to navigate · enter for details · 2 for files"),
		}
	default:
		banner = []string{styleDim.Render("Nothing to show here yet.")}
	}
	out = append(out, banner...)
	out = append(out, "")

	if len(files) == 0 {
		out = append(out, styleDim.Render("No changed files in this merge request."))
		return out
	}

	// Pick the first changed file and render its diff
	path := files[0]
	fd := m.diffForFirstChanged(path)
	out = append(out, styleSection.Render("CHANGED FILE — "+smartPath(path, w-22)))
	if fd == nil {
		out = append(out, styleDim.Render("Diff not available — open the Files view (2) for details."))
		return out
	}
	// Render first ~12 diff lines as a peek
	rows := fd.Lines
	maxRows := 12
	if len(rows) > maxRows {
		rows = rows[:maxRows]
	}
	for _, l := range rows {
		if l.Kind == DiffHunkHeader {
			out = append(out, cut(styleSel.Render(l.Text), w))
			continue
		}
		marker := " "
		col := ""
		switch l.Kind {
		case DiffAdd:
			marker, col = "+", green
		case DiffDel:
			marker, col = "-", red
		}
		line := fmt.Sprintf("%4s %s%s %s", numStr(l.NewNo), col, marker, l.Text)
		out = append(out, cut(line, w))
	}
	if len(fd.Lines) > maxRows {
		out = append(out, "")
		out = append(out, styleDim.Render(fmt.Sprintf("… %d more lines — open the file (2 → file) for full diff", len(fd.Lines)-maxRows)))
	}
	out = append(out, "")
	out = append(out, styleDim.Render(fmt.Sprintf("Tip: %d changed file(s) total — press 2 to browse", len(files))))
	return out
}

// diffForFirstChanged returns the FileDiff for the given path, or nil if
// the diff isn't loaded yet.
func (m *model) diffForFirstChanged(path string) *FileDiff {
	m.ensureDiff()
	return m.diffParsed[path]
}

// ---- detail pane ------------------------------------------------------------

// diffStripRows is the height of the always-visible diff strip.
func (m *model) diffStripRows() int {
	if m.narrow || m.height < 24 || m.tab == tabOverview {
		return 0 // hidden: narrow, too short, or already on the Diff tab
	}
	return 8
}

func (m *model) detailPaneView() []string {
	w := m.detailWidth() // inner pane width (bodyView handles borders)
	out := make([]string, 0, m.bodyHeight())

	hdr := "DETAILS"
	if m.focus == focusDiff {
		out = append(out, styleBorderF.Render(focusIndicator(true)+" "+hdr))
	} else {
		out = append(out, styleDim.Render(focusIndicator(false)+" "+hdr))
	}
	out = append(out, m.tabBarView(w)...)

	f := m.current()
	if f == nil {
		// No finding selected — render a productive empty state with the first
		// changed file's diff and a contextual banner instead of a placeholder.
		for _, l := range m.emptyStateContent(w) {
			out = append(out, cut(l, w))
		}
		return out
	}

	contentH := m.contentHeight()
	content := m.tabContent(m.tab)
	top := m.detailTop[m.tab]
	if top > len(content)-1 {
		top = max(0, len(content)-1)
		m.detailTop[m.tab] = top
	}
	for i := 0; i < contentH; i++ {
		idx := top + i
		if idx < len(content) {
			out = append(out, cut(content[idx], w))
		} else {
			out = append(out, "")
		}
	}

	// always-visible diff strip (wide terminals, non-Diff tabs)
	if strip := m.diffStripRows(); strip > 0 {
		fd := m.diffFor(f)
		out = append(out, styleBorder.Render(strings.Repeat("─", w)))
		if fd == nil {
			out = append(out, styleDim.Render(fmt.Sprintf("DIFF %s:%d — not in diff", f.File, f.Line)))
		} else {
			out = append(out, styleSection.Render(fmt.Sprintf("DIFF %s:%d  (Diff tab for the full view · +/- context)",
				smartPath(f.File, w-30), f.Line)))
			rows, hl := DiffWindow(fd, f.Side, f.Line, f.LineEnd, 2)
			texts := make([]string, len(rows))
			for i, r := range rows {
				texts[i] = expandTabs(r.Text)
			}
			hlTxt := HighlightedCodeLines(f.File, texts, plainMode())
			h := strip - 1
			start := 0
			if hl > h {
				start = hl - h/2
			}
			end := start + h
			if end > len(rows) {
				end = len(rows)
			}
			if start > 0 && start < end {
				start = end - h
			}
			if start < 0 {
				start = 0
			}
			for i := start; i < end; i++ {
				l := rows[i]
				marker, col := " ", ""
				switch l.Kind {
				case DiffAdd:
					marker, col = "+", green
				case DiffDel:
					marker, col = "-", red
				case DiffHunkHeader:
					out = append(out, cut(styleSel.Render(l.Text), w))
					continue
				}
				code := hlTxt[i]
				line := fmt.Sprintf("%4s %s%s %s", numStr(l.NewNo), col, marker, code)
				if i == hl {
					line = styleCursor.Render(cut(fmt.Sprintf("%4s %s%s %s", numStr(l.NewNo), col, marker, code), w))
				}
				out = append(out, cut(line, w))
			}
		}
	}
	return out
}

// m.overviewContent composes diff + evidence + explanation + discussion
// into one panel for the Overview tab (Chunk 2 §2). Avoids 5-tab switching
// while keeping each section discoverable and capped so the panel stays
// scannable on small terminals.
func (m *model) overviewContent(f *store.Finding, w int) []string {
	return m.overviewSections(f, w, true)
}

// overviewSections composes the finding header + sections. withDiff=false
// renders the right-pane SELECTED FINDING details region (findingdetails.go):
// full sections, independent scroll, pinned actions footer — the center diff
// pane already shows the diff, so no DIFF section here.
func (m *model) overviewSections(f *store.Finding, w int, withDiff bool) []string {
	if !withDiff {
		return m.findingDetailsRegion(f, w, m.findingDetailsRows())
	}
	var out []string
	out = append(out, fmt.Sprintf("%s %s · %s · :%d",
		sevChip(f), styleHeader.Render(f.ID), smartPath(f.File, max(10, w-30)), f.Line))
	if f.Publication != nil {
		out = append(out, stylePublished.Render(fmt.Sprintf("published → discussion %s", f.Publication.DiscussionID)))
	}
	// DIFF section (capped)
	out = append(out, "")
	out = append(out, styleSection.Render("DIFF"))
	if fd := m.diffFor(f); fd != nil {
		rows, _ := DiffWindow(fd, f.Side, f.Line, f.LineEnd, 2)
		const maxDiff = 12
		if len(rows) > maxDiff {
			rows = rows[:maxDiff]
		}
		texts := make([]string, len(rows))
		for i, r := range rows {
			texts[i] = expandTabs(r.Text)
		}
		hlTxt := HighlightedCodeLines(f.File, texts, plainMode())
		for i, r := range rows {
			if r.Kind == DiffHunkHeader {
				out = append(out, cut(styleSel.Render(r.Text), w))
				continue
			}
			marker := " "
			col := ""
			switch r.Kind {
			case DiffAdd:
				marker, col = "+", green
			case DiffDel:
				marker, col = "-", red
			}
			line := fmt.Sprintf("%4s %s%s %s", numStr(r.NewNo), col, marker, hlTxt[i])
			out = append(out, cut(line, w))
		}
		if len(fd.Lines) > maxDiff {
			out = append(out, styleDim.Render(fmt.Sprintf("… %d more lines — press gd for full diff", len(fd.Lines)-maxDiff)))
		}
	} else {
		out = append(out, styleDim.Render("(diff not in the changed set — file may be unmodified)"))
	}
	return append(out, m.overviewTailSections(f, w)...)
}

// overviewTailSections renders EXPLANATION + EVIDENCE + DISCUSSION (the
// sections after the DIFF block of overviewContent).
func (m *model) overviewTailSections(f *store.Finding, w int) []string {
	var out []string
	// EXPLANATION section
	out = append(out, "")
	out = append(out, styleSection.Render("EXPLANATION"))
	if strings.TrimSpace(f.Explanation) == "" {
		out = append(out, styleDim.Render("(no explanation yet — press d to ask the AI)"))
	} else {
		for _, l := range strings.Split(renderMarkdown(f.Explanation, w), "\n") {
			out = append(out, cut(l, w))
		}
	}
	// EVIDENCE section (capped at 6 lines)
	out = append(out, "")
	out = append(out, styleSection.Render("EVIDENCE"))
	if strings.TrimSpace(f.Evidence) == "" {
		out = append(out, styleDim.Render("(no file references)"))
	} else {
		n := 0
		for _, l := range strings.Split(f.Evidence, "\n") {
			if n >= 6 {
				out = append(out, styleDim.Render("…full text in Comment tab…"))
				break
			}
			out = append(out, cut(l, w))
			n++
		}
	}
	// DISCUSSION section (capped at 6 entries)
	if len(f.Discussion) > 0 {
		out = append(out, "")
		out = append(out, styleSection.Render("DISCUSSION"))
		n := 0
		for _, d := range f.Discussion {
			if n >= 6 {
				break
			}
			out = append(out, styleSel.Render("Q: ")+firstLine(d.Q))
			for _, l := range strings.Split(d.A, "\n") {
				out = append(out, "  "+cut(l, w))
			}
			n++
		}
	}
	return out
}

// masterDetailContent renders the selected finding's diff (right, 2/3) and
// explanation + evidence (left, 1/3) as two horizontal columns. Only called
// at >=120 cols (Chunk 2 §2 master-detail layout). Below 120 cols the
// caller falls back to overviewContent's vertical stacking.
func (m *model) masterDetailContent(f *store.Finding, w int) []string {
	const gap = 3 // divider column + 1 padding each side
	leftW := max(24, (w-gap)*1/3)
	rightW := w - leftW - gap

	// RIGHT: diff column (header + capped diff rows).
	right := make([]string, 0, 24)
	right = append(right, styleSection.Render(fmt.Sprintf("DIFF %s:%d", smartPath(f.File, max(8, rightW-12)), f.Line)))
	right = append(right, styleBorder.Render(strings.Repeat("─", rightW)))
	if fd := m.diffFor(f); fd != nil {
		rows, hl := DiffWindow(fd, f.Side, f.Line, f.LineEnd, 2)
		const maxDiff = 14
		if len(rows) > maxDiff {
			rows = rows[:maxDiff]
		}
		texts := make([]string, len(rows))
		for i, r := range rows {
			texts[i] = expandTabs(r.Text)
		}
		hlTxt := HighlightedCodeLines(f.File, texts, plainMode())
		for i, r := range rows {
			if r.Kind == DiffHunkHeader {
				right = append(right, cut(styleSel.Render(r.Text), rightW))
				continue
			}
			marker := " "
			col := ""
			switch r.Kind {
			case DiffAdd:
				marker, col = "+", green
			case DiffDel:
				marker, col = "-", red
			}
			line := fmt.Sprintf("%4s %s%s %s", numStr(r.NewNo), col, marker, hlTxt[i])
			if i == hl {
				line = styleCursor.Render(cut(fmt.Sprintf("%4s %s%s %s", numStr(r.NewNo), col, marker, hlTxt[i]), rightW))
			}
			right = append(right, cut(line, rightW))
		}
		if len(fd.Lines) > maxDiff {
			right = append(right, styleDim.Render("… full diff on the Diff tab (gd)"))
		}
	} else {
		right = append(right, styleDim.Render("(diff not in the changed set)"))
	}

	// LEFT: explanation + evidence column.
	left := make([]string, 0, 16)
	left = append(left, styleSection.Render(fmt.Sprintf("%s %s", sevChip(f), styleHeader.Render(f.ID))))
	if f.Publication != nil {
		left = append(left, stylePublished.Render(fmt.Sprintf("published → %s", f.Publication.DiscussionID)))
	}
	left = append(left, styleBorder.Render(strings.Repeat("─", leftW)))
	left = append(left, styleSection.Render("EXPLANATION"))
	if strings.TrimSpace(f.Explanation) == "" {
		left = append(left, styleDim.Render("(no explanation yet — press d to ask)"))
	} else {
		for _, l := range strings.Split(renderMarkdown(f.Explanation, leftW), "\n") {
			left = append(left, cut(l, leftW))
		}
	}
	left = append(left, "")
	left = append(left, styleSection.Render("EVIDENCE"))
	if strings.TrimSpace(f.Evidence) == "" {
		left = append(left, styleDim.Render("(no file references)"))
	} else {
		n := 0
		for _, l := range strings.Split(f.Evidence, "\n") {
			if n >= 8 {
				left = append(left, styleDim.Render("…"))
				break
			}
			left = append(left, cut(l, leftW))
			n++
		}
	}
	if len(f.Discussion) > 0 {
		left = append(left, "")
		left = append(left, styleSection.Render("DISCUSSION"))
		n := 0
		for _, d := range f.Discussion {
			if n >= 4 {
				break
			}
			left = append(left, styleSel.Render("Q: ")+firstLine(d.Q))
			for _, l := range strings.Split(d.A, "\n") {
				left = append(left, "  "+cut(l, leftW))
			}
			n++
		}
	}

	// Equalize column heights by padding the shorter one with blanks.
	for len(left) < len(right) {
		left = append(left, "")
	}
	for len(right) < len(left) {
		right = append(right, "")
	}

	// Pad each cell to its exact column width so JoinHorizontal aligns rows.
	padLeft := func(s string) string {
		if delta := leftW - lipgloss.Width(s); delta > 0 {
			return s + strings.Repeat(" ", delta)
		}
		return cut(s, leftW)
	}
	padRight := func(s string) string {
		if delta := rightW - lipgloss.Width(s); delta > 0 {
			return s + strings.Repeat(" ", delta)
		}
		return cut(s, rightW)
	}
	divider := styleBorder.Render("┃")
	out := make([]string, 0, len(left))
	for i := range left {
		out = append(out, lipgloss.JoinHorizontal(lipgloss.Top,
			padLeft(left[i]), " "+divider+" ", padRight(right[i])))
	}
	return out
}

// m.commentTabContent renders the comment editor / publish view (the
// original tabComment body — extracted so overviewContent can be composed
// without duplicating decision state + publication banner).
func (m *model) commentTabContent(f *store.Finding, w int) []string {
	var out []string
	out = append(out, styleSection.Render("COMMENT TO PUBLISH ON GITLAB"))
	if m.editingComment {
		out = append(out, styleWarn.Render("editing — Ctrl+S save · Esc cancel"))
		out = append(out, m.commentTA.View())
		return out
	}
	out = append(out, "decision: "+decisionStyle(f).Render(decisionLabel(f)))
	if f.Publication != nil {
		out = append(out, stylePublished.Render(fmt.Sprintf("published → discussion %s", f.Publication.DiscussionID)))
	}
	if strings.TrimSpace(f.EffectiveComment()) == "" {
		out = append(out, "")
		empty := centered("no comment yet — press e to write one", w, 4)
		out = append(out, empty...)
		return out
	}
	if strings.TrimSpace(f.EditedComment) != "" {
		out = append(out, styleWarn.Render("✎ manually edited — this exact text is published"))
	} else {
		out = append(out, styleDim.Render("(as suggested by the AI — press e to edit)"))
	}
	out = append(out, "")
	out = append(out, strings.Split(renderMarkdown(f.EffectiveComment(), w), "\n")...)
	return out
}

// ---- tab content ------------------------------------------------------------

func (m *model) tabContent(tab int) []string {
	f := m.current()
	if f == nil {
		// No finding selected — tabContent callers (header line above the tab
		// bar) want a single short banner line; the full empty state lives in
		// emptyStateContent() and is rendered by detailPaneView.
		return []string{styleDim.Render("Pick a finding — j/k to navigate, enter for details.")}
	}
	w := m.detailWidth()
	switch tab {
	case tabOverview:
		if m.width >= 120 && !m.narrow {
			return m.masterDetailContent(f, w)
		}
		return m.overviewContent(f, w)
	case tabComment:
		return m.commentTabContent(f, w)
	}
	return nil
}

// evidenceRefs extracts navigable file:line references from the evidence text.
var reRef = regexp.MustCompile(`[\w][\w./-]*\.(?:go|md|yaml|yml|json|sh|tf|toml)(:\d+)?`)

func evidenceRefs(f *store.Finding) []string {
	seen := map[string]bool{}
	var out []string
	for _, ref := range reRef.FindAllString(f.Evidence, 32) {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, styleSel.Render("  → ")+ref)
	}
	return out
}
