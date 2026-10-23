// findingdetails.go — SELECTED FINDING details region (right pane, bottom).
//
// The region renders the selected finding's full record: header (severity +
// title + ID), location, STATUS pill, EXPLANATION (Glamour-rendered),
// EVIDENCE and SUGGESTED COMMENT/FIX (full text), DISCUSSION Q/A, and a
// pinned actions footer. Content longer than the region scrolls behind a
// dedicated offset; the footer stays visible.
//
// Sub-scroll interaction model (predictable, documented):
//   - The LIST owns the pane focus keys: j/k/gg/G/Ctrl-d/Ctrl-u always move
//     the list cursor (nav.go) — never the details offset.
//   - The DETAILS region keeps its own scroll offset in
//     m.detailTop[fdScrollKey]. It auto-resets to the top whenever the
//     selected finding changes (tracked by underlying slice index in
//     m.detailTop[fdSelKey]) and is clamped to the content on every render.
//   - Dedicated scroll actions — findingsDetailsScroll / findingsDetailsPage
//     (1 line / half a region) — are wired to J/K + PageUp/PageDown via the
//     registry (see NEEDED_FOREIGN_CHANGES in the wave-2 findings report);
//     until then they are palette-reachable and unit-tested.
//   - `i` opens the inspector overlay as the full-text zoom (unchanged).
package tui

import (
	"fmt"
	"strings"

	"mr-review/internal/store"

	"charm.land/lipgloss/v2"
)

// detailTop keys beyond the tab indexes (tabCount == 2): the details region
// owns two extra slots. clampScrolls only iterates t < tabCount, so these
// are exclusively managed here.
const (
	fdScrollKey = 2 // details region scroll offset (first visible content row)
	fdSelKey    = 3 // underlying findings index the details region shows
)

// findingDetailsRows is the details-region height in rows: the pane interior
// minus the pane header, the list region, the separator and the "SELECTED
// FINDING" label (all rendered by renderFindingsPane in layout.go).
func (m *model) findingDetailsRows() int {
	if m.narrow {
		return 0 // narrow shows the list alone (listViewBody)
	}
	// interior = bodyHeight-2; list region (findingsListRows) + separator +
	// SELECTED FINDING label (2) — the details fill exactly what remains.
	rows := m.bodyHeight() - 2 - m.findingsListRows() - 2
	if rows < 4 {
		rows = 4
	}
	return rows
}

// syncFindingDetails resets the details scroll offset when the selection
// changes (by finding identity, not cursor index) and keeps the bookkeeping
// slot current. Pointer identity is exact for the live model; a reloaded
// slice (background ingest) gets a fresh offset, which is correct because
// the content underneath changed.
func (m *model) syncFindingDetails(f *store.Finding) {
	if m.detailTop == nil {
		m.detailTop = map[int]int{}
	}
	idx := -1
	for i := range m.findings {
		if &m.findings[i] == f {
			idx = i
			break
		}
	}
	if idx < 0 || idx != m.detailTop[fdSelKey] {
		m.detailTop[fdSelKey] = idx
		m.detailTop[fdScrollKey] = 0
	}
}

// findingsDetailsScroll moves the details region by delta rows (list cursor
// untouched). Upper clamp happens against the rendered content.
func (m *model) findingsDetailsScroll(delta int) {
	if m.current() == nil {
		return
	}
	if m.detailTop == nil {
		m.detailTop = map[int]int{}
	}
	top := m.detailTop[fdScrollKey] + delta
	if top < 0 {
		top = 0
	}
	m.detailTop[fdScrollKey] = top
}

// findingsDetailsPage scrolls the details region by roughly half a region
// (the PageUp/PageDown granularity).
func (m *model) findingsDetailsPage(dir int) {
	m.findingsDetailsScroll(dir * max(1, m.findingDetailsRows()-2))
}

// findingLocation renders the "file:line[:end] · side" line (unpadded line
// numbers — gutter padding belongs to the diff, not the location line).
func findingLocation(f *store.Finding, w int) string {
	loc := smartPath(f.File, max(10, w-14)) + fmt.Sprintf(":%d", f.Line)
	if f.LineEnd > f.Line {
		loc += fmt.Sprintf("-%d", f.LineEnd)
	}
	side := f.Side
	if side == "" {
		side = "new"
	}
	return loc + " · " + side + " side"
}

// findingStatusLine renders the STATUS pill row (decision + publication).
func findingStatusLine(f *store.Finding) string {
	line := "STATUS " + decisionStyle(f).Render("● "+decisionLabel(f))
	if f.Publication != nil {
		line += styleDim.Render(" → discussion " + f.Publication.DiscussionID)
	}
	return line
}

// findingDetailsContent builds the full (unwindowed) details region for f.
func (m *model) findingDetailsContent(f *store.Finding, w int) []string {
	var out []string

	// header: severity chip + title + ID
	title := truncPlain(f.Title, max(8, w-lipgloss.Width(sevChip(f))-10))
	out = append(out, sevChip(f)+" "+title+"  "+styleDim.Render(f.ID))
	// location: file:line[:end] + side marker
	out = append(out, cut(styleDim.Render(findingLocation(f, w)), w))
	// STATUS pill
	out = append(out, cut(findingStatusLine(f), w))
	out = append(out, "")

	// EXPLANATION — Glamour-rendered, wrapped to the pane width, full text.
	out = append(out, styleSection.Render("EXPLANATION"))
	if strings.TrimSpace(f.Explanation) == "" {
		out = append(out, styleDim.Render("(no explanation yet — press d to ask the AI)"))
	} else {
		for _, l := range strings.Split(renderMarkdown(f.Explanation, w), "\n") {
			out = append(out, cut(l, w))
		}
	}
	out = append(out, "")

	// EVIDENCE — full text (not capped), every line wrapped so nothing is
	// lost horizontally.
	out = append(out, styleSection.Render("EVIDENCE"))
	if strings.TrimSpace(f.Evidence) == "" {
		out = append(out, styleDim.Render("(no file references)"))
	} else {
		for _, l := range strings.Split(strings.TrimRight(f.Evidence, "\n"), "\n") {
			out = append(out, wrap(l, w)...)
		}
	}
	out = append(out, "")

	// SUGGESTED COMMENT / FIX — full text (or "—").
	out = append(out, styleSection.Render("SUGGESTED COMMENT"))
	if comment := strings.TrimSpace(f.EffectiveComment()); comment == "" {
		out = append(out, styleDim.Render("—"))
	} else {
		for _, l := range strings.Split(renderMarkdown(comment, w), "\n") {
			out = append(out, cut(l, w))
		}
	}
	if fix := strings.TrimSpace(f.Extra["suggested_fix"]); fix != "" {
		out = append(out, "")
		out = append(out, styleSection.Render("SUGGESTED FIX"))
		for _, l := range strings.Split(fix, "\n") {
			out = append(out, wrap(l, w)...)
		}
	}
	out = append(out, "")

	// DISCUSSION — Q/A pairs (full).
	if len(f.Discussion) > 0 {
		out = append(out, styleSection.Render("DISCUSSION"))
		for _, d := range f.Discussion {
			out = append(out, styleSel.Render("Q: ")+firstLine(d.Q))
			for _, l := range strings.Split(d.A, "\n") {
				for _, wl := range wrap(l, max(8, w-2)) {
					out = append(out, "  "+wl)
				}
			}
		}
		out = append(out, "")
	}
	return out
}

// findingActionRows builds the pinned actions footer, greedily wrapped to
// the pane width. The publish hint appears whenever anything (finding or
// ready draft comment) is publishable.
func (m *model) findingActionRows(w int) []string {
	segs := []string{"a approve", "r reject", "u undo", "e edit", "d discuss", "y copy"}
	if hasPublishable(m) {
		segs = append(segs, styleOK.Render("P publish preview"))
	}
	sep := styleDim.Render(" · ")

	var rows []string
	cur, curW := "", 0
	for _, seg := range segs {
		segW := lipgloss.Width(seg)
		add := segW
		if cur != "" {
			add += 3 // " · "
		}
		if cur != "" && curW+add > w {
			rows = append(rows, cur)
			cur, curW = "", 0
		}
		if cur == "" {
			cur, curW = seg, segW
		} else {
			cur += sep + seg
			curW += add
		}
	}
	if cur != "" {
		rows = append(rows, cur)
	}
	// scroll hint on the last footer row when the details content overflows
	if f := m.current(); f != nil {
		if c := len(m.findingDetailsContent(f, w)); c > m.findingDetailsRows()-len(rows) {
			rows[len(rows)-1] = cut(rows[len(rows)-1], max(8, w-10)) +
				styleDim.Render(fmt.Sprintf(" ↕ %d", c))
		}
	}
	return rows
}

// findingDetailsRegion renders the windowed details region: h rows total,
// the last rows pinned to the actions footer.
func (m *model) findingDetailsRegion(f *store.Finding, w, h int) []string {
	if w < 8 {
		w = 8
	}
	if h < 3 {
		h = 3
	}
	m.syncFindingDetails(f)
	content := m.findingDetailsContent(f, w)
	foot := m.findingActionRows(w)
	bodyH := h - len(foot)
	if bodyH < 1 {
		foot = foot[:1]
		bodyH = 1
	}
	maxTop := max(0, len(content)-bodyH)
	top := m.detailTop[fdScrollKey]
	if top > maxTop {
		top = maxTop
	}
	if top < 0 {
		top = 0
	}
	m.detailTop[fdScrollKey] = top

	rows := make([]string, 0, h)
	for i := top; i < top+bodyH; i++ {
		if i < len(content) {
			rows = append(rows, cut(content[i], w))
		} else {
			rows = append(rows, "")
		}
	}
	rows = append(rows, foot...)
	return rows
}

// findingDetailsEmpty is the details region's empty state (no finding
// selected). Layout.go renders its own one-line stub today; this richer
// block is provided for the wiring (see NEEDED_FOREIGN_CHANGES).
func (m *model) findingDetailsEmpty(w, h int) []string {
	var msg string
	switch {
	case m.bgRunning() && len(m.findings) == 0:
		msg = "AI review running — findings will appear here"
		if m.bgActivity != "" {
			msg += " · " + truncPlain(m.bgActivity, max(10, w-30))
		}
	case len(m.findings) == 0:
		msg = "no findings — review the diff manually"
	default:
		msg = "no finding selected — j/k in the findings list"
	}
	return centered(styleDim.Render(cut(msg, max(10, w-2))), w, max(1, h))
}

// ingestFindings swaps in a reloaded findings slice while keeping the
// selection stable by finding ID. Background ingest APPENDS findings; a
// pure index clamp (rebuild alone) silently moves the user's selection to
// whatever now sorts at the old index. Call this instead of rebuild when
// the findings slice itself is replaced (reload path).
func (m *model) ingestFindings(fs []store.Finding) {
	id := m.currentID() // stable ID survives the swap
	m.findings = fs
	m.rebuild()
	m.reselect(id)
}
