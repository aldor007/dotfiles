package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"path/filepath"
	"unicode/utf8"

	"mr-review/internal/store"
	"mr-review/internal/tui/widget"
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
	L := m.layout()
	if L.mode == layoutNarrow {
		return m.width - 2 // single box: borders only
	}
	return L.filesW
}

// detailWidth is the inner width of the narrow-mode detail pane.
func (m *model) detailWidth() int {
	L := m.layout()
	if L.mode == layoutNarrow {
		return m.width - 2 // single box: borders only
	}
	return L.diffW
}

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
	m.detailTop[tabOverview] = top
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
	title := truncPlain(rev.Title, max(28, m.width-86))
	// Wide layout (Chunk 5 step 3): breathing room — title row + progress row
	// with extra spacing. Title + AI status on line 1, progress + branch on line 2.
	line1 := styleHeader.Render(fmt.Sprintf("!%d · %s", rev.IID, title)) +
		"  " + styleDim.Render(rev.HeadSHA[:min(8, len(rev.HeadSHA))]) +
		"  " + ai
	line2 := "  " + branch + "  " + progress
	return cut(line1, m.width) + "\n" + cut(line2, m.width)
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
//
//	line 1: MODE · PANEL · counts · meta · (state message right-aligned)
//	line 2: contextual key hints (no longer dimmed — subtle bg tint instead)
//
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

// footerLine renders a single compact contextual footer that adapts to
// focus + leftView + curMode() (Chunk 3 §6). Replaces the old 2-line
// modeLine + keyHintLine pair. When m.bgRunning() is true, an AI status
// suffix is appended so background analysis is always visible.
func (m *model) footerLine() string {
	mode := m.modeLabel()
	panel := m.panelLabel()

	// Context-aware keyset derived from focus + leftView + curMode().
	var keys []string
	switch m.curMode() {
	case modeNormal:
		switch m.focus {
		case focusFiles, focusFindings:
			switch m.leftView {
			case viewFindings, viewFileFindings:
				keys = []string{"j/k", "enter", "a approve", "r reject", "e edit", "d discuss"}
			case viewFiles:
				keys = []string{"j/k", "enter", "1 findings", "3 comments", "4 findings"}
			case viewComments:
				keys = []string{"j/k", "enter", "e edit", "d delete"}
			}
		case focusDiff:
			keys = []string{"j/k", "[/] hunk", "c comment", "y copy"}
		}
	case modeSearch:
		keys = []string{"type to filter", "enter keep", "esc clear"}
	case modePalette, modeConfirm:
		keys = []string{"type to filter", "enter run", "esc close"}
	case modeOverlay:
		keys = []string{"j/k", "enter select", "esc close"}
	case modeAsk:
		keys = []string{"type question", "enter send", "esc cancel"}
	case modeDraftEdit, modeFindingEdit:
		keys = []string{"edit markdown", "Ctrl+S save", "esc cancel"}
	case modeExport:
		keys = []string{"type path", "Ctrl+S write", "esc cancel"}
	default:
		keys = []string{"j/k", "enter", "? help", ": commands"}
	}

	var b strings.Builder
	b.WriteString(styleSection.Render(mode))
	b.WriteString(styleDim.Render(" · "))
	b.WriteString(styleSel.Render(panel))
	b.WriteString(styleDim.Render("    "))
	b.WriteString(styleDim.Render(strings.Join(keys, " · ")))

	// AI status suffix
	if m.bgReview != nil && m.bgReview.Status != store.AgentFailed && m.bgReview.Status != store.AgentInterrupted {
		b.WriteString(styleDim.Render("    · AI: "))
		switch m.bgReview.Status {
		case store.AgentRunning:
			b.WriteString(styleWarn.Render("analyzing"))
		default:
			b.WriteString(styleOK.Render("done"))
		}
		if n := len(m.findings); n > 0 {
			b.WriteString(styleDim.Render(fmt.Sprintf(" (%d)", n)))
		}
	}
	return cut(b.String(), m.width)
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

// keyHintLine renders footer line 2 as a full-width, subtly-tinted hint
// bar so the keys read as a control surface, not buried gray text. Under
// NO_COLOR the bg is dropped and the line is plain text (some terminals
// disable true-color backgrounds entirely when NO_COLOR is set).
func (m *model) keyHintLine() string {
	keys := widget.RenderHints(m.footerHints(), max(0, m.width-2))
	body := "┃ " + cut(keys, max(0, m.width-2))
	if plainMode() {
		return body
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color("236")). // very dark dim gray — subtle bar
		Render(pad(body, m.width))
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

// panelLabel is the focused pane name (or the tuicr workspace state).
func (m *model) panelLabel() string {
	if wsPane != nil {
		if wsDetail && m.current() != nil {
			return "FINDING DETAIL"
		}
		return "COCKPIT"
	}
	switch m.focus {
	case focusFiles:
		return "FILES"
	case focusDiff:
		return "DIFF"
	}
	if m.leftView == viewComments {
		return "COMMENTS"
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

// View renders the full screen. Overlays FLOAT: the normal frame
// (header + body + footer) renders exactly as it would with no overlay,
// then the overlay box is composited over it (see overlay.go).
func (m *model) View() tea.View {
	if m.width == 0 {
		return tea.NewView("loading…")
	}
	frame := m.normalFrame()
	box, opts := m.activeOverlayBox()
	if box == nil {
		v := tea.NewView(frame)
		v.AltScreen = true
		if wsPane != nil {
			v.MouseMode = tea.MouseModeCellMotion // mouse works in the tuicr pane
		}
		return v
	}
	opts.width = m.width
	opts.plain = plainMode()
	v := tea.NewView(compositeOverlay(strings.Split(frame, "\n"), box, opts))
	v.AltScreen = true
	if wsPane != nil {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// normalFrame renders the full-screen body exactly as with no overlay open.
func (m *model) normalFrame() string {
	var b strings.Builder
	b.WriteString(m.headerView() + "\n")
	body := m.bodyView()
	if wsPane != nil {
		body = m.wsView(m.width, m.bodyHeight())
	}
	b.WriteString(body + "\n")
	b.WriteString(m.footerView())
	return b.String()
}

// activeOverlayBox returns the bordered box lines of the topmost overlay
// (nil when none). State gates (palette/draft editor/export) mirror the
// old View switch; simple overlays go through the stack top. On medium
// terminals with findings focus the floating findings drawer is the "overlay".
func (m *model) activeOverlayBox() ([]string, overlayOpts) {
	var opts overlayOpts
	switch {
	case m.paletteOn:
		return m.paletteView(m.width, m.bodyHeight()), opts
	case m.draftEdit != nil:
		opts.anchorLow = true // editor floats low: diff anchor stays visible above
		return m.draftEditorView(m.width, m.bodyHeight()), opts
	case m.exportDlg != nil:
		return m.exportDialogView(m.width, m.bodyHeight()), opts
	}
	switch m.topOverlay() { // active modal: the overlay stack top
	case ovInspector:
		return m.inspectorView(m.width, m.bodyHeight()), opts
	case ovHelp:
		return m.helpOverlay(m.width, m.bodyHeight()), opts
	case ovPublish:
		return m.publishPreviewOverlay(m.width, m.bodyHeight()), opts
	case ovSummary:
		return overlayBoxLines(RenderSummary(m.rev, m.findings, m.comments), m.width, m.bodyHeight(), "s/Esc close"), opts
	case ovQueue:
		return m.publishQueueView(m.width, m.bodyHeight()), opts
	case ovViewer:
		return m.viewerView(m.width, m.bodyHeight()), opts
	}
	// medium mode: findings focus opens the right-edge drawer over the body
	if !m.zoom && m.layout().mode == layoutMedium && m.focus == focusFindings && m.topOverlay() == ovNone {
		dw := findingsDrawerWidth(m.width)
		rows := m.renderFindingsPane(dw, m.bodyHeight())
		opts.anchorRight = true
		return strings.Split(m.renderPaneBox(rows, dw, true), "\n"), opts
	}
	return nil, opts
}

// bodyView renders the bordered body:
//
//	narrow: single focused pane (files / diff / findings via Tab · 1 · 2 · 3)
//	medium: compact files sidebar + diff (findings = floating drawer)
//	wide:   FILES | DIFF | FINDINGS side by side, 1-cell dividers
//
// zoom (z) maximizes the focused pane.
func (m *model) bodyView() string {
	h := m.bodyHeight()
	L := m.layout()
	if m.zoom {
		var rows []string
		switch m.focus {
		case focusFiles:
			rows = m.renderFilesPane(m.width-2, h)
		case focusFindings:
			rows = m.renderFindingsPane(m.width-2, h)
		default:
			rows = m.renderDiffPane(m.width-2, h)
		}
		return m.renderPaneBox(rows, m.width-2, true)
	}
	switch L.mode {
	case layoutNarrow:
		var rows []string
		switch m.focus {
		case focusFiles:
			rows = m.filesView()
		case focusFindings:
			rows = m.navigatorPaneRows(h)
		default:
			rows = m.detailPaneView()
		}
		return m.renderPaneBox(rows, m.width-2, true)
	case layoutMedium:
		return joinPanes(h,
			m.renderPaneBox(m.renderFilesPane(L.filesW, h), L.filesW, m.focus == focusFiles),
			m.renderPaneBox(m.renderDiffPane(L.diffW, h), L.diffW, m.focus == focusDiff))
	default: // wide: three simultaneous panes side by side
		return joinPanes(h,
			m.renderPaneBox(m.renderFilesPane(L.filesW, h), L.filesW, m.focus == focusFiles),
			m.renderPaneBox(m.renderDiffPane(L.diffW, h), L.diffW, m.focus == focusDiff),
			m.renderPaneBox(m.renderFindingsPane(L.findingsW, h), L.findingsW, m.focus == focusFindings))
	}
}

// joinPanes composes pane boxes side by side with a 1-column gap. Every pane
// box is exactly bodyHeight rows tall (renderPaneBox guarantees it), so the
// result is a single full-width block that never overflows vertically.
func joinPanes(h int, boxes ...string) string {
	parts := make([]string, 0, len(boxes)*2)
	gap := strings.Repeat(" \n", h)
	gap = gap[:len(gap)-1] // drop trailing newline
	for i, b := range boxes {
		if i > 0 {
			parts = append(parts, gap)
		}
		parts = append(parts, b)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
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
	if d := w - cellWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func cut(s string, w int) string {
	if cellWidth(s) <= w {
		return s
	}
	return keepCells(s, w)
}

// cellWidth counts printable cells: ANSI escape sequences (CSI/SGR and other
// C1-introduced sequences) contribute 0 cells. Tab counts as one cell here;
// code paths that need tabstop-accurate layout expand tabs first (expandTabs).
// This mirrors skipCells' parser and exists because lipgloss.Width
// undercounts lines dense with per-token SGR resets (observed -4 cells on
// chroma-highlighted diff rows), which made pane boxes overflow.
func cellWidth(s string) int {
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
		cells += cw
		i += size
	}
	return cells
}

// keepCells returns the first w printable cells of s, preserving ANSI escape
// sequences (the cell-exact counterpart of skipCells).
func keepCells(s string, w int) string {
	if w <= 0 {
		return ""
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
		if cells+cw > w {
			return s[:i]
		}
		cells += cw
		i += size
	}
	return s
}

// expandTabs replaces tab characters with spaces up to the next 4-column
// tabstop (measured from the start of the string). Call it on PLAIN diff
// text before styling so gutter alignment and width math stay true.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	col := 0
	for _, r := range s {
		if r == '\t' {
			n := 4 - col%4
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col++
	}
	return b.String()
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
