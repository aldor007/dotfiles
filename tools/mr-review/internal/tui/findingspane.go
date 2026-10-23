package tui

import (
	"fmt"

	"mr-review/internal/store"
	"mr-review/internal/tui/widget"

	"charm.land/lipgloss/v2"
)

// findingCard renders one finding as 2 compact lines:
//
//	row 1: severity · ID · file:line            (severity-coloured)
//	row 2: title (truncated)                    (dim, monospace-friendly)
//
// selected findings get a severity-coloured left accent bar + subtle bg
// tint; unselected findings get a thin ── separator. The two-line format
// is what makes 20+ findings scannable on a 35-row terminal.
func (m *model) findingCard(f *store.Finding, w int, selected bool) []string {
	sev := sevChip(f) // pre-styled chip (severity colour + bold)
	breadcrumb := smartPath(f.File, w/2) + ":" + numStr(f.Line)

	row1 := fmt.Sprintf("%s %s · %s", sev, styleDim.Render(f.ID), styleDim.Render(breadcrumb))

	// pill word is the longest decision label; reserve 2 spaces + 9 cells
	// for it so cut(row2, w) (byte-based) never chops it. ANSI escapes in
	// the styled pill add ~14 bytes but 0 cells, so they don't tighten the
	// title budget — they only widen the safety margin against cut().
	const pillWord = "● published" // widest decision word (9 cells)
	titleCells := max(8, w-lipgloss.Width(pillWord)-2)
	title := truncPlain(f.Title, titleCells)

	// pill is plain text (no ANSI) so cut(row2, w) — byte-counting —
	// doesn't chop the word. The severity color in row1 carries the visual
	// signal; the pill word alone conveys decision state.
	var pill string
	switch {
	case f.Publication != nil:
		pill = "● published"
	case f.Stale:
		pill = "● stale"
	case f.Decision == "approved":
		pill = "● approved"
	case f.Decision == "rejected":
		pill = "● rejected"
	default:
		pill = "● pending"
	}
	// shrink the title further to leave cell budget for the pill + 2-space gap
	// selected: row2 fits w cells (bar takes 1 elsewhere). unselected: a
	// dim "─" prefix is prepended later, so reserve 1 cell for it here.
	title = truncPlain(title, max(8, w-lipgloss.Width(pill)-3))
	row2 := title + "  " + pill

	lines := []string{cut(row1, w), cut(row2, w)}
	if selected {
		// Chunk 5 §3: stronger selected highlight — severity-color ▌ accent +
		// Background(236) tint + bold + brighter foreground (not just reverse).
		bar := styleBorderF.Render("▌")
		var tinted lipgloss.Style
		if plainMode() {
			tinted = lipgloss.NewStyle().Bold(true) // NO_COLOR: shape only
		} else {
			tinted = lipgloss.NewStyle().Bold(true).
				Background(lipgloss.Color("236")).
				Foreground(lipgloss.Color("252"))
		}
		for i := range lines {
			lines[i] = bar + tinted.Render(pad(cut(stripANSIish(lines[i]), w-1), w-1))
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

// ---- region geometry -------------------------------------------------------
// The findings pane (right, wide / medium drawer / zoom) is split into two
// sub-regions that scroll independently:
//
//	┌ FINDINGS ────────────────┐  ← paneHeader (layout.go)
//	│ FINDINGS n/m · k pending │  ← list header row (findingsViewAt)
//	│ … 2-line finding cards … │  ← list region, findingsListRows() rows
//	├──────────────────────────┤  ← separator (layout.go)
//	│ SELECTED FINDING         │  ← region label (layout.go)
//	│ … details sections …     │  ← details region, findingDetailsRows()
//	└ actions footer (pinned) ─┘
//
// findingsListRows is the list-region height in ROWS (including the list
// header row). It must stay in sync with renderFindingsPane (layout.go):
// listH = max(4, h*45/100) where h = bodyHeight at every call site.
func (m *model) findingsListRows() int {
	if m.narrow {
		// narrow mode shows the bare list view (no pane header row above it,
		// but findingsViewAt carries its own header row).
		h := m.bodyHeight() - 3
		if h < 3 {
			h = 3
		}
		return h
	}
	return max(4, m.bodyHeight()*45/100)
}

// findingsPaneWidth is the inner width of the pane that currently shows the
// findings list: the right pane on wide terminals, the floating drawer on
// medium, the full single pane on narrow, and the zoomed pane under `z`.
func (m *model) findingsPaneWidth() int {
	if m.zoom && m.focus == focusFindings {
		return m.width - 2
	}
	return m.findingsWidth()
}

// findingsVisCards is the number of 2-line cards fully visible in the list
// region. clampScrolls (view.go) derives the same number from listCardLines,
// so the scroll window and the renderer always agree.
func (m *model) findingsVisCards() int {
	vis := (m.findingsListRows() - 1) / 2 // minus the list header row
	if vis < 1 {
		vis = 1
	}
	return vis
}

// listCardLines is the visible card-line budget in the findings list region
// (clampScrolls divides by 2 for 2-line cards). Region-aware: the right-pane
// list is only ~45% of the pane, not the full body height.
func (m *model) listCardLines() int { return m.findingsListRows() - 1 }

// listVisibleRows is the number of visible findings cards (scroll math:
// halfScroll moves listVisibleRows()/2 cards per half-page).
func (m *model) listVisibleRows() int { return m.findingsVisCards() }

// syncFindingsListWindow keeps m.listTop (first visible card) consistent
// with the cursor. cursorDown/cursorUp do not call clampScrolls, so the
// render path maintains the window here — same rule as clampScrolls:
// cursor ∈ [listTop, listTop+visCards). Also bounds listTop when findings
// shrink or a stale window survives a filter change.
func (m *model) syncFindingsListWindow() {
	n := len(m.filtered)
	if n == 0 {
		m.listTop = 0
		return
	}
	vis := m.findingsVisCards()
	if m.listTop > n-1 {
		m.listTop = n - 1
	}
	if m.listTop < 0 {
		m.listTop = 0
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
	return widget.RenderTabBar(names, active, m.focus == focusFindings, plainMode(), w, styles)
}

func (m *model) listView() []string {
	if m.narrow && m.focus == focusDiff {
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

// findingsView renders the findings list at the pane's real geometry.
// Narrow mode shows this view alone (listViewBody); wide/medium/zoom render
// it inside renderFindingsPane's list region.
func (m *model) findingsView() []string {
	return m.findingsViewAt(m.findingsPaneWidth(), m.findingsListRows())
}

// findingsListHeader is the focus-aware list header row:
// "FINDINGS visible/total · k pending · filter: …".
func (m *model) findingsListHeader(w int) string {
	pending := 0
	for _, idx := range m.filtered {
		f := &m.findings[idx]
		if f.Decision == "pending" && f.Publication == nil && !f.Stale {
			pending++
		}
	}
	hdr := fmt.Sprintf("FINDINGS %d/%d", len(m.filtered), len(m.findings))
	if pending > 0 {
		hdr += fmt.Sprintf(" · %d pending", pending)
	}
	if m.filter.Kind != "all" || m.filter.Severity != "" || m.filter.File != "" {
		hdr += " · filter: " + m.filter.Describe()
	}
	if m.focus == focusFindings {
		return styleBorderF.Render(cut(focusIndicator(true)+" "+hdr, w))
	}
	return styleDim.Render(cut(focusIndicator(false)+" "+hdr, w))
}

// findingsViewAt renders the findings list sized to (w, listRows): one
// header row + up to listRows-1 rows of 2-line finding cards. The card
// window starts at m.listTop; the cursor card is always inside the window.
func (m *model) findingsViewAt(w, listRows int) []string {
	if w < 8 {
		w = 8
	}
	if listRows < 1 {
		listRows = 1
	}
	m.syncFindingsListWindow()
	out := []string{m.findingsListHeader(w)}
	cardLines := listRows - 1 // header row consumed above

	if len(m.filtered) == 0 {
		out = append(out, m.findingsEmptyState(w, cardLines)...)
		return m.padRows(out, w, listRows)
	}

	used := 0
	for ci := m.listTop; ci < len(m.filtered) && used < cardLines; ci++ {
		f := &m.findings[m.filtered[ci]]
		for _, l := range m.findingCard(f, w, ci == m.cursor) {
			if used >= cardLines {
				break
			}
			out = append(out, cut(l, w))
			used++
		}
	}
	return m.padRows(out, w, listRows)
}

// findingsEmptyState is the list region's empty state: AI progress while the
// background review runs, a manual-review hint when there are none, and a
// filter hint when the filter hides everything.
func (m *model) findingsEmptyState(w, h int) []string {
	filtered := m.filter.Kind != "all" || m.filter.Severity != "" || m.filter.File != ""
	msg := "(no findings)"
	var extra string
	switch {
	case filtered && len(m.findings) > 0:
		if m.filter.Kind == "pending" {
			msg = "no pending findings — review complete (press f to see all)"
		} else {
			msg = "no findings match the filter — press f to change"
		}
	case filtered:
		// filter active AND zero findings total: the filter hint is the
		// actionable one (it tells the user how to widen the view again)
		msg = "no findings match the filter — press f to change"
	case m.bgRunning():
		msg = "AI review running in the background — browse the diff now (2 → enter); findings appear here automatically"
		if m.bgActivity != "" {
			extra = truncPlain(m.bgActivity, max(10, w-2))
		}
	case len(m.findings) == 0:
		msg = "no findings — review the diff manually (2 → enter)"
	}
	lines := centered(msg, w, min(5, max(1, h)))
	if extra != "" && len(lines) > 1 {
		lines[len(lines)-2] = styleDim.Render(cut(extra, w))
	}
	return lines
}

func (m *model) padRows(rows []string, w, h int) []string {
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
	if m.focus == focusFindings && m.leftView == viewFileFindings {
		out = append(out, styleBorderF.Render(focusIndicator(true)+" "+hdr))
	} else {
		out = append(out, styleDim.Render(focusIndicator(false)+" "+hdr))
	}

	if m.fileFindingsFile == "" {
		out = append(out, centered("press 2, then Enter on a file", w, h-2)...)
		return m.padRows(out, w, h)
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
		return m.padRows(out, w, h)
	}

	type card struct {
		idx   int
		lines []string
	}
	var cards []card
	for _, idx := range idxs {
		f := &m.findings[idx]
		sel := m.leftView == viewFileFindings && m.focus == focusFindings &&
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
	return m.padRows(out, w, h)
}
