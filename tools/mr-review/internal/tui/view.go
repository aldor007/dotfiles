package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"mr-review/internal/store"
	"mr-review/internal/tui/widget"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Style variables live in theme.go (resolved from the active theme).

func sevStyle(sev string) lipgloss.Style {
	if s, ok := styleSeverity[sev]; ok {
		return s
	}
	return lipgloss.NewStyle()
}

func decisionLabel(f *store.Finding) string {
	switch {
	case f.Publication != nil:
		return "published"
	case f.Stale:
		return "stale"
	default:
		return f.Decision
	}
}

func decisionStyle(f *store.Finding) lipgloss.Style {
	if f.Publication != nil {
		return stylePublished
	}
	if s, ok := styleDecision[f.Decision]; ok {
		return s
	}
	return styleDim
}

func decisionMarker(f *store.Finding) string {
	switch {
	case f.Publication != nil:
		return icons.DecPublished
	case f.Decision == "approved":
		return icons.DecApproved
	case f.Decision == "rejected":
		return icons.DecRejected
	case f.Stale:
		return icons.DecStale
	}
	return icons.DecPending
}

// ---- geometry --------------------------------------------------------------

func (m *model) bodyHeight() int {
	h := m.height - 6 // header(2) + status(1) + keys(1) + box borders(2)
	if h < 6 {
		h = 6
	}
	return h
}

func (m *model) listWidth() int {
	if m.narrow {
		return m.width - 2 // single box: borders only
	}
	w := m.width * 34 / 100
	if w < 32 {
		w = 32
	}
	if w > 56 {
		w = 56
	}
	if w > m.width-30 {
		w = max(26, m.width-30)
	}
	return w
}

func (m *model) detailWidth() int {
	if m.narrow {
		return m.width - 2 // single box: borders only
	}
	w := m.width - m.listWidth() - 3 // divider + box borders (inner width)
	if w < 28 {
		w = 28
	}
	return w
}

// contentHeight is the exact number of content rows under the pane header
// and tab bar (and above the diff strip on wide terminals).
func (m *model) contentHeight() int {
	h := m.bodyHeight() - 6 // pane header(1) + tab bar(3) + box borders(2)
	if s := m.diffStripRows(); s > 0 {
		h -= s + 1 // strip separator + strip rows
	}
	if h < 3 {
		h = 3
	}
	return h
}

// ---- scrolling -------------------------------------------------------------

func (m *model) scrollBy(delta int) {
	top := m.detailTop[m.tab] + delta
	if top < 0 {
		top = 0
	}
	m.detailTop[m.tab] = top
}

func (m *model) setScroll(n int) { m.detailTop[m.tab] = n }

func (m *model) clampScrolls() {
	h := m.contentHeight() // visible rows under the tab bar
	if m.detailTop == nil {
		m.detailTop = map[int]int{}
	}
	for t := 0; t < tabCount; t++ {
		max := len(m.tabContent(t)) - h
		if max < 0 {
			max = 0
		}
		if m.detailTop[t] > max {
			m.detailTop[t] = max
		}
		if m.detailTop[t] < 0 {
			m.detailTop[t] = 0
		}
	}
	vis := m.listCardLines() / 2 // findingCard is now 2 lines (severity row + title row)
	if vis < 1 {
		vis = 1
	}
	if m.cursor < m.listTop {
		m.listTop = m.cursor
	}
	if m.cursor >= m.listTop+vis {
		m.listTop = m.cursor - vis + 1
	}
	if m.listTop < 0 {
		m.listTop = 0
	}
}

// syncDiffScroll centers the diff highlight for the current finding.
func (m *model) syncDiffScroll() {
	f := m.current()
	if f == nil {
		return
	}
	fd := m.diffFor(f)
	if fd == nil {
		return
	}
	_, hl := DiffWindow(fd, f.Side, f.Line, f.LineEnd, m.ctxLines)
	h := m.bodyHeight() - 6
	if h < 3 {
		h = 3
	}
	top := hl - h/2
	if top < 0 {
		top = 0
	}
	m.detailTop[tabDiff] = top
}

func (m *model) ensureDiff() {
	if m.diffParsed != nil {
		return
	}
	b, err := os.ReadFile(filepath.Join(m.dir, "diff.patch"))
	if err != nil {
		m.diffParsed = map[string]*FileDiff{}
	} else {
		m.diffParsed = ParseDiff(string(b), 20000)
	}
	m.diffVersion++
	m.invalidateBrowseDoc()
}

func (m *model) diffFor(f *store.Finding) *FileDiff {
	m.ensureDiff()
	return m.diffParsed[f.File]
}

// ---- finding cards (left pane) ---------------------------------------------

// smartPath keeps the filename + as many trailing parent dirs as fit.
func smartPath(path string, maxLen int) string {
	if maxLen <= 0 || len(path) <= maxLen {
		return path
	}
	base := filepath.Base(path)
	if len(base)+2 > maxLen {
		return "…" + base[len(base)-(maxLen-2):]
	}
	keep := maxLen - len(base) - 2 // "…/" + base
	dir := filepath.Dir(path)
	if len(dir) > keep {
		dir = dir[len(dir)-keep:]
		if i := strings.Index(dir, "/"); i >= 0 && i < len(dir)-1 {
			dir = dir[i+1:] // avoid cutting mid-segment
		}
	}
	return "…/" + dir + "/" + base
}

// findingCard renders one finding as 2 compact lines:
//   row 1: severity · ID · file:line            (severity-coloured)
//   row 2: title (truncated)                    (dim, monospace-friendly)
// selected findings get a severity-coloured left accent bar + subtle bg
// tint; unselected findings get a thin ── separator. The two-line format
// is what makes 20+ findings scannable on a 35-row terminal.
func (m *model) findingCard(f *store.Finding, w int, selected bool) []string {
	sev := sevChip(f) // pre-styled chip (severity colour + bold)
	breadcrumb := smartPath(f.File, w/2) + ":" + numStr(f.Line)
	title := truncPlain(f.Title, max(12, w-4))

	row1 := fmt.Sprintf("%s %s · %s", sev, styleDim.Render(f.ID), styleDim.Render(breadcrumb))
	row2 := title

	if f.Publication != nil {
		row2 += "  " + stylePublished.Render("● published")
	} else if f.Stale {
		row2 += "  " + styleWarn.Render("● stale")
	} else if f.Decision == "approved" {
		row2 += "  " + styleOK.Render("● approved")
	} else if f.Decision == "rejected" {
		row2 += "  " + styleDecision["rejected"].Render("● rejected")
	} else {
		row2 += "  " + styleDecisionPending().Render("● pending")
	}

	lines := []string{cut(row1, w), cut(row2, w)}
	if selected {
		// left-edge severity accent bar + cursor background (selectable)
		bar := styleBorderF.Render("▌")
		for i := range lines {
			lines[i] = bar + styleCursor.Render(pad(cut(stripANSIish(lines[i]), w-1), w-1))
		}
	} else {
		// thin separator so cards read as a list without taking extra rows
		lines[1] = styleDim.Render("─") + lines[1]
	}
	return lines
}

// styleDecisionPending returns the muted-yellow style for the "pending"
// decision pill. Defined as a helper so we don't litter map access.
func styleDecisionPending() lipgloss.Style { return styleDecision["pending"] }

func (m *model) listCardLines() int {
	// approximate card height (2 or 3 lines) for scroll math
	return m.bodyHeight() - 2 // pane header + padding
}

// listVisibleRows approximates the visible navigator rows (scroll math).
func (m *model) listVisibleRows() int {
	h := m.bodyHeight() - 2
	if h < 1 {
		return 1
	}
	return h
}

// viewTabNames are the left-pane view tabs (the 4th is contextual).
var viewTabNamesFull = []string{"FINDINGS", "FILES", "IN FILE", "COMMENTS"}
var viewTabNamesShort = []string{"FIND", "FILES", "FILE", "COMM"}

func (m *model) leftViewTabRow(w int) []string {
	active := m.leftView
	names := viewTabNamesFull
	// narrow left pane: drop to short labels so the 4-tab bar fits without
	// the widget's last-resort "…" drop (which would hide COMMENTS).
	if widget.TabBarWidth(viewTabNamesFull) > w {
		names = viewTabNamesShort
	}
	// the IN FILE tab is contextual: present only when a file has been
	// selected in FILES view (non-workspace mode). Otherwise we render a
	// dim placeholder so the bar width stays stable (no jitter on toggle).
	if m.fileFindingsFile == "" {
		// mutate via a copy so the cached slice isn't shared
		cp := make([]string, len(names))
		copy(cp, names)
		cp[2] = "───"
		names = cp
	}
	styles := widget.TabBarStyles{
		Border: styleBorder, BorderFocus: styleBorderF, Dim: styleDim,
		Active: styleTabActive, Cursor: styleCursor,
	}
	return widget.RenderTabBar(names, active, m.focus == focusList, plainMode(), w, styles)
}

func (m *model) listView() []string {
	if m.narrow && m.focus == focusDetail {
		return nil
	}
	inner := m.listViewBody()
	tabs := m.leftViewTabRow(m.listWidth())
	if len(tabs) == 0 {
		return inner
	}
	// the tab bar's own bottom border (RenderTabBar's third row) already
	// separates it from the body — no extra separator here.
	out := append([]string{}, tabs...)
	out = append(out, inner...)
	return out
}

// listViewBody returns just the per-view content (no tabs, no header).
func (m *model) listViewBody() []string {
	switch m.leftView {
	case viewFiles:
		return m.filesView()
	case viewComments:
		return m.commentsView()
	case viewFileFindings:
		return m.fileFindingsView()
	}
	return m.findingsView()
}

// findingsView renders the AI findings cards (the classic left pane).
func (m *model) findingsView() []string {
	w := m.listWidth()
	h := m.bodyHeight() - 2
	var out []string
	hdr := fmt.Sprintf("FINDINGS %d/%d", len(m.filtered), len(m.findings))
	if m.filter.Kind != "all" || m.filter.Severity != "" || m.filter.File != "" {
		hdr += " · filter: " + m.filter.Describe()
	}
	if m.focus == focusList {
		out = append(out, styleBorderF.Render(focusIndicator(true)+" "+hdr))
	} else {
		out = append(out, styleDim.Render(focusIndicator(false)+" "+hdr))
	}

	if len(m.filtered) == 0 {
		msg := "(no findings)"
		if m.bgRunning() {
			msg = "AI review running in the background — browse the diff now (2 → enter); findings appear here automatically"
		}
		if m.filter.Kind != "all" || m.filter.Severity != "" || m.filter.File != "" {
			if m.filter.Kind == "pending" {
				msg = "no pending findings — review complete (press f to see all)"
			} else {
				msg = "no findings match the filter — press f to change"
			}
		}
		out = append(out, centered(msg, w, h-2)...)
		return m.padRows(out, h)
	}

	// card-based scrolling
	type card struct {
		idx   int
		lines []string
	}
	var cards []card
	for _, idx := range m.filtered {
		f := &m.findings[idx]
		cards = append(cards, card{idx, m.findingCard(f, w, idx == m.filtered[m.cursor])})
	}
	// window of cards fitting h-2
	start := 0
	if m.cursor >= 0 {
		// find cursor card offset
		off := 0
		for i, c := range cards {
			if c.idx == m.filtered[m.cursor] {
				if off > h-3 {
					start = i
				}
				break
			}
			off += len(c.lines)
		}
	}
	used := 0
	for i := start; i < len(cards) && used < h-2; i++ {
		c := cards[i]
		for _, l := range c.lines {
			if used >= h-2 {
				break
			}
			out = append(out, cut(l, w))
			used++
		}
	}
	return m.padRows(out, h)
}

func (m *model) padRows(rows []string, h int) []string {
	w := m.listWidth()
	for len(rows) < h {
		rows = append(rows, "")
	}
	for i := range rows {
		rows[i] = pad(cut(rows[i], w), w)
	}
	return rows
}

// fileFindingsView shows the findings for the selected file (viewFileFindings).
// Activated from the FILES view by Enter on a file row (non-workspace mode only).
func (m *model) fileFindingsView() []string {
	w := m.listWidth()
	h := m.bodyHeight() - 2
	var out []string
	hdr := "FILE: " + shortPath(m.fileFindingsFile)
	if m.fileFindingsFile == "" {
		hdr = "IN FILE — pick a file in FILES (2), then Enter"
	}
	if m.focus == focusList && m.leftView == viewFileFindings {
		out = append(out, styleBorderF.Render(focusIndicator(true)+" "+hdr))
	} else {
		out = append(out, styleDim.Render(focusIndicator(false)+" "+hdr))
	}

	if m.fileFindingsFile == "" {
		out = append(out, centered("press 2, then Enter on a file", w, h-2)...)
		return m.padRows(out, h)
	}

	// collect the findings for this file (respect global filters: severity/text)
	var idxs []int
	for _, idx := range m.filtered {
		if m.findings[idx].File == m.fileFindingsFile {
			idxs = append(idxs, idx)
		}
	}
	if len(idxs) == 0 {
		out = append(out, centered(fmt.Sprintf("(no findings for %s)", shortPath(m.fileFindingsFile)), w, h-2)...)
		return m.padRows(out, h)
	}

	type card struct {
		idx   int
		lines []string
	}
	var cards []card
	for _, idx := range idxs {
		f := &m.findings[idx]
		sel := m.leftView == viewFileFindings && m.focus == focusList &&
			idx == idxs[min(max(m.fileFindCur, 0), len(idxs)-1)]
		cards = append(cards, card{idx, m.findingCard(f, w, sel)})
	}

	used := 0
	for _, c := range cards {
		if used >= h-2 {
			break
		}
		for _, l := range c.lines {
			if used >= h-2 {
				break
			}
			out = append(out, cut(l, w))
			used++
		}
	}
	return m.padRows(out, h)
}

func centered(msg string, w, h int) []string {
	var out []string
	padTop := (h - 1) / 2
	if padTop < 0 {
		padTop = 0
	}
	for i := 0; i < padTop; i++ {
		out = append(out, "")
	}
	out = append(out, styleDim.Render(pad(cut(msg, max(4, w-2)), max(4, w-2))))
	for len(out) < h {
		out = append(out, "")
	}
	return out
}

// ---- segmented tab bar ------------------------------------------------------

var tabShortNames = []string{"DIFF", "EXPL", "EVID", "COMM", "DISC"}

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
	// shrink on narrow detail panes: short labels first (the widget handles
	// the last-resort "…" drop and the stretch-to-width)
	if widget.TabBarWidth(names) > w && !m.narrow {
		names = tabShortNames
	}
	return widget.RenderTabBar(names, m.tab, m.focus == focusDetail, plainMode(), w,
		widget.TabBarStyles{
			Border: styleBorder, BorderFocus: styleBorderF, Dim: styleDim,
			Active: styleTabActive, Cursor: styleCursor,
		})
}

// ---- detail pane ------------------------------------------------------------

// diffStripRows is the height of the always-visible diff strip.
func (m *model) diffStripRows() int {
	if m.narrow || m.height < 24 || m.tab == tabDiff {
		return 0 // hidden: narrow, too short, or already on the Diff tab
	}
	return 8
}

func (m *model) detailPaneView() []string {
	w := m.detailWidth() // inner pane width (bodyView handles borders)
	out := make([]string, 0, m.bodyHeight())

	hdr := "DETAILS"
	if m.focus == focusDetail {
		out = append(out, styleBorderF.Render(focusIndicator(true)+" "+hdr))
	} else {
		out = append(out, styleDim.Render(focusIndicator(false)+" "+hdr))
	}
	out = append(out, m.tabBarView(w)...)

	f := m.current()
	if f == nil {
		empty := centered("no finding selected — press Tab, then j/k", w, m.contentHeight())
		out = append(out, empty...)
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
				texts[i] = r.Text
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

// ---- tab content ------------------------------------------------------------

func (m *model) tabContent(tab int) []string {
	f := m.current()
	if f == nil {
		empty := centered("no finding selected — press Tab, then j/k", m.detailWidth(), m.bodyHeight())
		return empty
	}
	w := m.detailWidth()
	switch tab {
	case tabDiff:
		return m.diffTabContent(f, w)
	case tabExplanation:
		if strings.TrimSpace(f.Explanation) == "" {
			empty := centered("no explanation available", w, m.bodyHeight())
			return empty
		}
		return append([]string{styleSection.Render("EXPLANATION")},
			strings.Split(renderMarkdown(f.Explanation, w), "\n")...)
	case tabEvidence:
		var out []string
		out = append(out, styleSection.Render("EVIDENCE"))
		refs := evidenceRefs(f)
		if len(refs) == 1 && strings.Contains(refs[0], "no file references") {
			empty := centered("no file references in the evidence — press d to ask the AI", w, 6)
			out = append(out, empty...)
		} else {
			out = append(out, refs...)
		}
		out = append(out, "")
		out = append(out, wrap(f.Evidence, w)...)
		return out
	case tabComment:
		var out []string
		out = append(out, styleSection.Render("COMMENT TO PUBLISH ON GITLAB"))
		if m.editingComment {
			out = append(out, styleWarn.Render("editing — Ctrl+S save · Esc cancel"))
			out = append(out, m.commentTA.View())
			return out
		}
		// decision state is always visible — even when the comment body is
		// empty, the user needs to see whether the finding is approved,
		// published, etc. before deciding whether to write one.
		out = append(out, "decision: "+decisionStyle(f).Render(decisionLabel(f)))
		if f.Publication != nil {
			out = append(out, stylePublished.Render(
				fmt.Sprintf("published → discussion %s", f.Publication.DiscussionID)))
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
		out = append(out, "")
		out = append(out, "decision: "+decisionStyle(f).Render(decisionLabel(f)))
		if f.Publication != nil {
			out = append(out, stylePublished.Render(
				fmt.Sprintf("published → discussion %s", f.Publication.DiscussionID)))
		}
		return out
	case tabDiscussion:
		var out []string
		out = append(out, styleSection.Render("DISCUSSION WITH THE AI"))
		if len(f.Discussion) == 0 {
			e1 := centered("No discussion yet", w, 4)
			e2 := centered("Press d to ask AI about this finding.", w, 2)
			out = append(out, e1...)
			out = append(out, e2...)
			return out
		}
		for _, d := range f.Discussion {
			out = append(out, styleSel.Render("Q: ")+firstLine(d.Q))
			for _, l := range strings.Split(renderMarkdown(d.A, w), "\n") {
				out = append(out, "  "+l)
			}
			out = append(out, "")
		}
		return out
	}
	return nil
}

// diffTabContent: header + focused, syntax-highlighted diff window.
func (m *model) diffTabContent(f *store.Finding, w int) []string {
	if m.browseFile != "" { // continuous diff (manual review mode)
		return m.browseDiffContent(w)
	}
	fd := m.diffFor(f)
	if fd == nil {
		out := []string{styleSection.Render("DIFF")}
		out = append(out, centered(fmt.Sprintf("%s:%d — file not part of this diff (v opens the worktree file)", f.File, f.Line), w, m.bodyHeight()-4)...)
		return out
	}
	rows, hl := DiffWindow(fd, f.Side, f.Line, f.LineEnd, m.ctxLines)
	plain := plainMode()

	texts := make([]string, len(rows))
	for i, r := range rows {
		texts[i] = r.Text
	}
	hlTexts := HighlightedCodeLines(f.File, texts, plain)

	all := RenderDiffLines(rows, f.Side, hl, w, plain, hlTexts)
	if m.diffHOff > 0 { // horizontal scroll: skip leading cells, ANSI-aware
		for i := range all {
			all[i] = skipCells(all[i], m.diffHOff)
		}
	}

	h := m.contentHeight() - 1 // header line
	if h < 3 {
		h = 3
	}
	top := m.detailTop[tabDiff]
	if top > len(all)-h {
		top = max(0, len(all)-h)
	}
	if top < 0 {
		top = 0
	}
	m.detailTop[tabDiff] = top
	end := top + h
	if end > len(all) {
		end = len(all)
	}
	hint := fmt.Sprintf("±%d ctx · +/- adjust · [/] hunk · , . scroll →", m.ctxLines)
	if m.diffHOff > 0 {
		hint = fmt.Sprintf("→ +%d cols · ±%d ctx · , . scroll", m.diffHOff, m.ctxLines)
	}
	out := []string{styleSection.Render(fmt.Sprintf("DIFF %s:%d  %s",
		smartPath(f.File, w-lipgloss.Width(hint)-14), f.Line, hint))}
	out = append(out, all[top:end]...)
	return out
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

// ---- chrome ----------------------------------------------------------------

// headerView renders a single rich info bar (or 2 stacked rows on narrow
// terminals): MR identity · title · branch→base · AI status · progress.
// Layout priority: identity > branch > AI > progress. Counts shrink with
// available width; the MR ID + title are never truncated past 24 chars.
func (m *model) headerView() string {
	rev := m.rev
	ap, pe, rj, pub := m.decisionCounts()

	// progress pill: "N approved / M pending" — always shown
	progress := styleSel.Render(fmt.Sprintf("✓ %d / ⏳ %d", ap, pe))
	if rj > 0 {
		progress += styleDim.Render(fmt.Sprintf(" / ✗ %d", rj))
	}
	if pub > 0 {
		progress += stylePublished.Render(fmt.Sprintf(" / ◆ %d", pub))
	}

	// AI pill: running / done / idle — one cell so the eye reads it instantly
	ai := m.headerAIPill()

	branch := styleBranch.Render(rev.SourceBranch + " → " + rev.TargetBranch)
	if m.narrow {
		title := truncPlain(rev.Title, max(16, m.width-18))
		line1 := styleHeader.Render(fmt.Sprintf("!%d", rev.IID)) +
			" " + styleDim.Render(rev.HeadSHA[:min(8, len(rev.HeadSHA))]) +
			" " + title
		line2 := ai + "  " + branch + "  " + progress
		return cut(line1, m.width) + "\n" + cut(line2, m.width)
	}
	title := truncPlain(rev.Title, max(24, m.width-72))
	line1 := styleHeader.Render(fmt.Sprintf("!%d · %s", rev.IID, title)) +
		"  " + styleDim.Render(rev.HeadSHA[:min(8, len(rev.HeadSHA))]) +
		"  " + ai +
		"  " + branch +
		"  " + progress
	return cut(line1, m.width)
}

// headerAIPill renders the AI status as a single short label with a colour
// cue (warn=pending, ok=done, err=failed, dim=idle). One cell, never wraps.
func (m *model) headerAIPill() string {
	switch {
	case m.bgReview == nil:
		return styleDim.Render("AI idle")
	case m.bgReview.Status == store.AgentRunning:
		return styleWarn.Render("AI running") + styleDim.Render(fmt.Sprintf(" · %d raw", m.bgRaw))
	case m.bgReview.Status == store.AgentDone:
		return styleOK.Render("AI done")
	case m.bgReview.Status == store.AgentFailed:
		return styleErr.Render("AI failed")
	case m.bgReview.Status == store.AgentInterrupted:
		return styleWarn.Render("AI interrupted")
	}
	return styleDim.Render("AI idle")
}

func (m *model) decisionCounts() (approved, pending, rejected, published int) {
	for i := range m.findings {
		f := &m.findings[i]
		switch {
		case f.Publication != nil:
			published++
		case f.Decision == "approved":
			approved++
		case f.Decision == "rejected":
			rejected++
		default:
			pending++
		}
	}
	return
}

// statusBarView: separate status line (state ≠ key hints).
func (m *model) statusBarView() string {
	ap, pe, rj, pub := m.decisionCounts()
	switch {
	case m.busy:
		return styleWarn.Render(fmt.Sprintf("Status: agent running · %ds",
			int(time.Since(m.busySince).Seconds())))
	case m.askingID != "":
		return styleWarn.Render(fmt.Sprintf("Status: asking AI about %s · %ds · keep triaging (Esc cancels)",
			m.askingID, int(time.Since(m.askingSince).Seconds())))
	}
	// sticky background-review states (failed/interrupted must not be
	// masked by transient feedback)
	if m.bgReview != nil {
		switch m.bgReview.Status {
		case store.AgentFailed, store.AgentInterrupted:
			return m.bgStatusLine()
		}
	}
	switch {
	case m.editingComment:
		return styleWarn.Render("Status: editing GitLab comment (Ctrl+S saves)")
	case m.draftEdit != nil:
		loc := "general MR comment"
		if !m.draftEdit.general {
			loc = m.draftEdit.file + fmt.Sprintf(":%d", m.draftEdit.line)
		}
		return styleWarn.Render("Status: writing comment at " + loc + " (Ctrl+S saves · Esc cancels)")
	case m.exportDlg != nil:
		return styleWarn.Render("Status: export dialog (Ctrl+S writes a local file)")
	case m.paletteOn:
		return styleWarn.Render(fmt.Sprintf("Status: command palette · %d actions", len(m.paletteItems)))
	case m.searchOn:
		return styleWarn.Render("Status: search (/) — enter keeps · esc clears")
	case m.visualOn:
		lo, hi := m.visualRange()
		return styleWarn.Render(fmt.Sprintf("Status: range selected (%d lines) — c comment · esc cancel", hi-lo+1))
	case m.pendingKey != "":
		return styleWarn.Render("Status: pending «g» — gg top · gd diff · gn next unreviewed")
	case m.confirmDelete:
		return styleWarn.Render("Status: delete this draft? y = delete · n = keep")
	}
	if m.bgRunning() && m.msg == "" {
		return m.bgStatusLine() // background review progress (transient msgs win)
	}
	if m.msg != "" {
		kind := styleSel
		switch {
		case strings.Contains(m.msg, "failed") || strings.Contains(m.msg, "error"):
			kind = styleErr
		case strings.Contains(m.msg, "cancelled") || strings.Contains(m.msg, "kept"):
			kind = styleDim
		case strings.Contains(m.msg, "review complete"):
			kind = styleOK
		}
		return widget.RenderStatusLine("Status: ", []widget.StatusSegment{
			{Text: truncPlain(m.msg, max(10, m.width-12)), Style: &kind},
		}, 0, nil)
	}
	if len(m.findings) > 0 && pe == 0 {
		return styleOK.Render(fmt.Sprintf("Status: review complete · %d approved · %d rejected · %d published — P to publish",
			ap, rj, pub))
	}
	if ap > 0 {
		return styleOK.Render(fmt.Sprintf("Status: ready to publish · %d approved, %d pending (P)", ap, pe))
	}
	return styleDim.Render(fmt.Sprintf("Status: reviewing · %d pending", pe))
}

// footerView: 2 prominent lines.
//   line 1: MODE · PANEL · counts · meta · (state message right-aligned)
//   line 2: contextual key hints (no longer dimmed — subtle bg tint instead)
// Transient state messages overlay line 1 as the last segment; line 1's
// mode+panel prefix is never replaced so the user always sees where they are.
func (m *model) footerView() string {
	var b strings.Builder
	b.WriteString(m.modeLine() + "\n")
	if m.confirmClean {
		b.WriteString("┃ " + styleWarn.Render("remove worktree? y = remove · n = keep (review state is kept)"))
		return b.String()
	}
	if m.confirmDelete {
		b.WriteString("┃ " + styleWarn.Render("delete this draft? y = delete · n = keep"))
		return b.String()
	}
	if m.editingComment {
		b.WriteString("┃ " + styleWarn.Render("editing comment") + styleDim.Render(" · Ctrl+S save · Esc cancel"))
		return b.String()
	}
	if m.inputOn {
		b.WriteString("┃ " + m.input.View() + styleDim.Render("  Enter=send · Esc=cancel"))
		return b.String()
	}
	if m.searchOn {
		b.WriteString("┃ " + m.searchInput.View() + styleDim.Render("  Enter=apply · Esc=clear"))
		return b.String()
	}
	// the keys bar is derived from the registry (footerHints); render in a
	// subtle bg tint so it reads as a hint bar, not buried gray text.
	b.WriteString(m.keyHintLine())
	return b.String()
}

// modeLine renders footer line 1: active MODE + PANEL + counts + meta,
// followed by the transient status message (right-aligned) when present.
// Width-aware: at narrow widths, the meta segment collapses; the mode+panel
// prefix is always preserved.
func (m *model) modeLine() string {
	mode := m.modeLabel()
	panel := m.panelLabel()
	var counts string
	if n := len(m.findings); n > 0 {
		counts = fmt.Sprintf("%d findings", n)
		if pe := m.pendingCount(); pe > 0 && pe < n {
			counts += fmt.Sprintf(" · %d unresolved", pe)
		}
	} else {
		counts = "no findings yet"
	}
	meta := styleDim.Render("? help · : commands · q quit")
	prefix := styleSection.Render(mode) + styleDim.Render(" · ") +
		styleSel.Render(panel) + styleDim.Render(" · ") +
		counts
	line := prefix + styleDim.Render("  ") + meta

	if msg := m.statusBarView(); msg != "" {
		// strip ANSI for width math so the right-aligned slice lines up
		gap := max(2, m.width-lipgloss.Width(prefix)-lipgloss.Width(stripANSIish(msg))-12)
		if gap < 2 {
			gap = 2
		}
		line += styleDim.Render(strings.Repeat(" ", gap)) + msg
	}
	return cut(line, max(0, m.width-1))
}

// keyHintLine renders footer line 2: the contextual key hints wrapped in a
// subtle bg-tint bar so the keys read as a control surface, not noise.
func (m *model) keyHintLine() string {
	keys := widget.RenderHints(m.footerHints(), max(0, m.width-4))
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).
		Render("┃ " + cut(keys, max(0, m.width-4)))
	return bar
}

// modeLabel maps the active inputMode to a short display label, with a
// colour cue (warn for input modes, ok for normal). The label is the
// single source of truth for the "where am I?" answer.
func (m *model) modeLabel() string {
	switch m.curMode() {
	case modeNormal:
		return styleOK.Render("NORMAL")
	case modePalette:
		return styleWarn.Render("COMMAND")
	case modeSearch:
		return styleWarn.Render("SEARCH")
	case modeDraftEdit, modeFindingEdit:
		return styleWarn.Render("EDIT")
	case modeExport:
		return styleWarn.Render("EXPORT")
	case modePublish:
		return styleWarn.Render("PUBLISH")
	case modeOverlay:
		return styleWarn.Render("OVERLAY")
	case modeAsk:
		return styleWarn.Render("ASK")
	case modeConfirm:
		return styleWarn.Render("CONFIRM")
	}
	return styleDim.Render("NORMAL")
}

// panelLabel is the current left-pane view, or the focus pane name. Helps
// the user answer "what am I looking at?" in one glance.
func (m *model) panelLabel() string {
	if wsPane != nil {
		if wsDetail && m.current() != nil {
			return "FINDING DETAIL"
		}
		return "COCKPIT"
	}
	if m.narrow {
		if m.focus == focusDetail {
			return "DETAIL"
		}
		return "LIST"
	}
	if m.focus == focusDetail {
		return "DETAIL"
	}
	switch m.leftView {
	case viewFindings:
		return "FINDINGS"
	case viewFiles:
		return "FILES"
	case viewComments:
		return "COMMENTS"
	case viewFileFindings:
		return "IN FILE"
	}
	return "FINDINGS"
}

// pendingCount counts findings still requiring human attention.
func (m *model) pendingCount() int {
	n := 0
	for i := range m.findings {
		f := &m.findings[i]
		if f.Publication == nil && f.Decision == "pending" && !f.Stale {
			n++
		}
	}
	return n
}

// ---- main View -------------------------------------------------------------

// View renders the full screen.
func (m *model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("loading…")
	}
	var b strings.Builder

	// modal screens that replace the body. The palette, draft editor and
	// export dialog keep their own state gates (paletteOn / draftEdit /
	// exportDlg — the palette is also a stack member, ovPalette); the
	// simple overlays render by stack top (topOverlay).
	switch {
	case m.paletteOn:
		b.WriteString(m.headerView() + "\n")
		b.WriteString(m.paletteView(m.width, m.bodyHeight()))
		b.WriteString(m.footerView())
		return tea.NewView(b.String())
	case m.draftEdit != nil:
		b.WriteString(m.headerView() + "\n")
		b.WriteString(m.draftEditorView(m.width, m.bodyHeight()))
		b.WriteString(m.footerView())
		return tea.NewView(b.String())
	case m.exportDlg != nil:
		b.WriteString(m.headerView() + "\n")
		b.WriteString(m.exportDialogView(m.width, m.bodyHeight()))
		b.WriteString(m.footerView())
		return tea.NewView(b.String())
	}
	switch m.topOverlay() { // active modal: the overlay stack top (mirrored in m.overlay)
	case ovInspector:
		b.WriteString(m.headerView() + "\n")
		b.WriteString(m.inspectorView(m.width, m.bodyHeight()))
		b.WriteString(m.footerView())
		return tea.NewView(b.String())
	case ovHelp:
		b.WriteString(m.headerView() + "\n")
		b.WriteString(m.helpOverlay(m.width, m.bodyHeight()))
		b.WriteString(m.footerView())
		return tea.NewView(b.String())
	case ovPublish:
		b.WriteString(m.headerView() + "\n")
		b.WriteString(m.publishPreviewOverlay(m.width, m.bodyHeight()))
		b.WriteString(m.footerView())
		return tea.NewView(b.String())
	case ovSummary:
		b.WriteString(m.headerView() + "\n")
		b.WriteString(overlayBox(RenderSummary(m.rev, m.findings, m.comments), m.width, m.bodyHeight(), "s/Esc close"))
		b.WriteString(m.footerView())
		return tea.NewView(b.String())
	}
	// ovFilter and a closed ovPalette have no dedicated body (unchanged
	// behavior): the normal layout renders underneath.

	b.WriteString(m.headerView() + "\n")
	body := m.bodyView()
	if wsPane != nil {
		body = m.wsView(m.width, m.bodyHeight())
	}
	b.WriteString(body + "\n")
	b.WriteString(m.footerView())
	v := tea.NewView(b.String())
	v.AltScreen = true
	if wsPane != nil {
		v.MouseMode = tea.MouseModeCellMotion // mouse works in the tuicr pane
	}
	return v
}

// bodyView renders the bordered body: narrow = single pane; wide = two
// panes with a continuous divider; zoom = details pane only.
func (m *model) bodyView() string {
	w := m.width
	h := m.bodyHeight()
	borderCol := styleBorder
	if m.focus == focusDetail {
		borderCol = styleBorderF
	}
	top := borderCol.Render("╭" + strings.Repeat("─", w-2) + "╮")
	bottom := borderCol.Render("╰" + strings.Repeat("─", w-2) + "╯")
	inner := h - 2

	var rows []string
	if m.zoom && !m.narrow {
		rowsZ := m.detailPaneView()
		for i := 0; i < inner; i++ {
			if i < len(rowsZ) {
				rows = append(rows, "│"+pad(cut(rowsZ[i], w-2), w-2)+"│")
			} else {
				rows = append(rows, "│"+strings.Repeat(" ", w-2)+"│")
			}
		}
		return top + "\n" + strings.Join(rows, "\n") + "\n" + bottom
	}
	if m.narrow {
		var rowsN []string
		if m.focus == focusList {
			rowsN = m.listView()
		} else {
			rowsN = m.detailPaneView()
		}
		for i := 0; i < inner; i++ {
			if i < len(rowsN) {
				rows = append(rows, "│"+pad(cut(rowsN[i], w-2), w-2)+"│")
			} else {
				rows = append(rows, "│"+strings.Repeat(" ", w-2)+"│")
			}
		}
		return top + "\n" + strings.Join(rows, "\n") + "\n" + bottom
	}

	lw := m.listWidth()
	rw := w - lw - 3 // borders + divider
	left := m.listView()
	right := m.detailPaneView()
	for len(left) < inner {
		left = append(left, "")
	}
	for len(right) < inner {
		right = append(right, "")
	}
	// divider between left and right panes — visually heavier than the
	// outer frame so the two widgets read as distinct boxes
	div := styleBorder.Render("┃")
	for i := 0; i < inner; i++ {
		l := pad(cut(left[i], lw), lw)
		r := pad(cut(right[i], rw), rw) // pad: the right border must stay aligned
		rows = append(rows, "│"+l+div+r+"│")
	}
	return top + "\n" + strings.Join(rows, "\n") + "\n" + bottom
}

// ---- shared helpers --------------------------------------------------------

func overlayBox(content string, w, h int, hint string) string {
	lines := strings.Split(content, "\n")
	lines = append(lines, "", styleDim.Render(hint))
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	inner := w - 2
	var b strings.Builder
	b.WriteString("┌" + strings.Repeat("─", inner) + "┐\n")
	for _, l := range lines {
		b.WriteString("│" + pad(cut(l, inner), inner) + "│\n")
	}
	b.WriteString("└" + strings.Repeat("─", inner) + "┘\n")
	return b.String()
}

// helpOverlay renders the keyboard reference derived from the action
// registry (plus the fixed mode/invariant sections), so help can never drift
// from the real bindings. When m.helpFilter is non-empty, the body is
// live-filtered to actions/fixed-entries that match the substring (case
// insensitive, on key+desc+name+id).
func (m *model) helpOverlay(w, h int) string {
	var b strings.Builder
	b.WriteString(styleHeader.Render("KEYBOARD REFERENCE") + "")
	b.WriteString(styleDim.Render("  ·  / filter · esc close") + "\n")
	if m.helpFilterOn {
		b.WriteString(styleDim.Render("filter: "))
		b.WriteString(m.helpFilterInput.View())
		b.WriteString("\n")
	} else {
		b.WriteString("\n")
	}

	q := strings.ToLower(strings.TrimSpace(m.helpFilter))
	matchFixed := func(s string) bool { return q == "" || strings.Contains(strings.ToLower(s), q) }
	matchAction := func(a Action) bool {
		if q == "" {
			return true
		}
		if strings.Contains(strings.ToLower(a.ID), q) {
			return true
		}
		if strings.Contains(strings.ToLower(a.Desc), q) {
			return true
		}
		if strings.Contains(strings.ToLower(a.Name), q) {
			return true
		}
		for _, kk := range a.Keys {
			if strings.Contains(strings.ToLower(kk), q) {
				return true
			}
		}
		return false
	}

	b.WriteString(styleSection.Render("MODES & SAFETY") + "\n")
	for _, k := range [][]string{
		{"esc", "cancel: selection / search / zoom / browse — never publishes"},
		{":", "command palette (searchable actions, same handlers as keys)"},
		{"P", "publication preview → publisher asks to type PUBLISH"},
		{"1/2/3", "left pane: findings / changed files / comment navigator"},
		{"i / z", "inspect finding · zoom the details pane"},
	} {
		if !matchFixed(k[0]) && !matchFixed(k[1]) {
			continue
		}
		b.WriteString(fmt.Sprintf("  %-12s %s\n", styleSel.Render(k[0]), k[1]))
	}
	b.WriteString("\n")

	acts := enabledActions(m)
	lastCat := Category("")
	for _, a := range acts {
		if a.ID == "view.help" { // documenting the help key inside help
			continue
		}
		if !matchAction(a) {
			continue
		}
		if a.Cat != lastCat {
			lastCat = a.Cat
			b.WriteString(styleSection.Render(strings.ToUpper(string(a.Cat))) + "\n")
		}
		label := a.KeyLabel()
		if label == "" {
			label = "(palette)"
		}
		b.WriteString(fmt.Sprintf("  %-12s %s\n", styleSel.Render(label), a.Desc))
	}
	return b.String()
}

// publishPreviewOverlay shows exactly what P would hand to the publisher:
// approved AI findings and ready manual comments, each with its destination.
func (m *model) publishPreviewOverlay(w, h int) string {
	var approved []store.Finding
	for i := range m.findings {
		f := &m.findings[i]
		if f.Decision == "approved" && f.Publication == nil && !f.Stale {
			approved = append(approved, *f)
		}
	}
	var ready []store.DraftComment
	for i := range m.comments {
		c := &m.comments[i]
		if c.State == store.CommentReady && c.Publication == nil {
			ready = append(ready, *c)
		}
	}
	var b strings.Builder
	b.WriteString(styleSection.Render("PUBLICATION PREVIEW — nothing is sent yet") + "\n\n")
	fmt.Fprintf(&b, "target: !%d %s on %s\n", m.rev.IID, m.rev.Project, m.rev.Host)
	fmt.Fprintf(&b, "reviewed head: %s (publisher re-verifies before posting)\n\n", short(m.rev.HeadSHA))
	if stale := countStale(m.findings); stale > 0 {
		b.WriteString(styleWarn.Render(fmt.Sprintf("⚠ %d stale finding(s) — excluded from this batch\n\n", stale)))
	}
	if len(approved) == 0 && len(ready) == 0 {
		b.WriteString(styleWarn.Render("nothing to publish: approve findings or write comments first") + "\n")
	}
	if len(approved) > 0 {
		b.WriteString(styleSection.Render(fmt.Sprintf("AI findings (%d)", len(approved))) + "\n")
	}
	for _, f := range approved {
		fmt.Fprintf(&b, "%s %s %s:%d\n", sevStyle(f.Severity).Render(f.Severity), f.ID, f.File, f.Line)
		for i, l := range wrap(f.EffectiveComment(), w-4) {
			if i >= 2 {
				b.WriteString(styleDim.Render("    … (full text in the Comment tab)\n"))
				break
			}
			b.WriteString("    " + l + "\n")
		}
		b.WriteString("\n")
	}
	if len(ready) > 0 {
		b.WriteString(styleSection.Render(fmt.Sprintf("Manual comments (%d)", len(ready))) + "\n")
	}
	for _, c := range ready {
		loc := c.Location()
		if c.General {
			loc = "(general MR comment)"
		}
		fmt.Fprintf(&b, "%s %s %s\n", styleOK.Render(""), c.ID, loc)
		for i, l := range wrap(c.Text, w-4) {
			if i >= 2 {
				b.WriteString(styleDim.Render("    … (full text in the comment navigator)\n"))
				break
			}
			b.WriteString("    " + l + "\n")
		}
		b.WriteString("\n")
	}
	total := len(approved) + len(ready)
	for _, l := range wrap(fmt.Sprintf("%d comment(s) would be posted — the publisher asks for a final PUBLISH confirmation", total), w-2) {
		b.WriteString(cut(styleWarn.Render(l), w) + "\n")
	}
	return b.String()
}

func countStale(fs []store.Finding) int {
	n := 0
	for i := range fs {
		if fs[i].Stale {
			n++
		}
	}
	return n
}

// ---- text helpers ----------------------------------------------------------

func wrap(s string, w int) []string {
	w = max(20, w)
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			if line == "" {
				line = word
			} else if lipgloss.Width(line)+1+lipgloss.Width(word) <= w {
				line += " " + word
			} else {
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func pad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func cut(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

// skipCells drops the first n printable cells of s, preserving ANSI escape
// sequences (used for horizontal scrolling in the diff view).
func skipCells(s string, n int) string {
	if n <= 0 {
		return s
	}
	cells := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		cw := 1
		if r >= 0x1100 && (r <= 0x115F || r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF) || (r >= 0xAC00 && r <= 0xD7A3) ||
			(r >= 0xF900 && r <= 0xFAFF) || (r >= 0xFE30 && r <= 0xFE6F) ||
			(r >= 0xFF00 && r <= 0xFF60) || (r >= 0xFFE0 && r <= 0xFFE6) ||
			(r >= 0x1F300 && r <= 0x1F64F)) {
			cw = 2
		}
		if cells+cw > n {
			return s[i:]
		}
		cells += cw
		i += size
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
