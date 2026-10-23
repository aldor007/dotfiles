// layout.go — three-pane layout + focus model (foundations chunk).
//
// Wide (>=120 cols): FILES | DIFF | FINDINGS side by side, the continuous
// diff centered as the primary workspace. Medium (80–119): compact files
// sidebar + diff; findings float in as a right-anchored drawer when focused.
// Narrow (<80): a single focused pane; Tab / 1 / 2 / 3 switch panes.
package tui

import "strings"

// Focus targets: exactly one pane has focus at a time.
const (
	focusFiles = iota
	focusDiff
	focusFindings
)

type paneLayout int

const (
	layoutWide paneLayout = iota
	layoutMedium
	layoutNarrow
)

// layoutResult is the resolved pane geometry for the current width.
type layoutResult struct {
	mode      paneLayout
	filesW    int // files pane inner width (0 = hidden)
	diffW     int // diff pane inner width
	findingsW int // findings pane inner width (0 = hidden/drawer)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// pane chrome constants: every pane is an independent bordered box
// (2 border columns) separated by a 1-column gap. panesChrome(n) = total
// columns consumed by borders + gaps for n panes.
func panesChrome(n int) int { return n*2 + (n - 1) }

// layout resolves the three-pane geometry. Wide: filesW = clamp(0.22w, 26, 44),
// findingsW = clamp(0.30w, 34, 56), diffW = the rest, never below 40.
// Width budget per pane INCLUDES its own 2 border columns + 1 gap column
// between panes (panesChrome), so side-by-side boxes sum exactly to w.
func (m *model) layout() layoutResult {
	w := m.width
	switch {
	case w >= 120:
		fw := clampInt(w*22/100, 26, 44)
		gw := clampInt(w*30/100, 34, 48)   // 48 max: keeps the diff pane ≥100 cells at 200 cols
		dw := w - fw - gw - panesChrome(3) // 3 borders pairs + 2 gaps
		if dw < 40 {
			dw = 40
			gw = w - fw - dw - panesChrome(3)
			if gw < 34 {
				gw = 34
				fw = w - gw - dw - panesChrome(3)
				if fw < 26 {
					fw = 26
				}
			}
		}
		return layoutResult{layoutWide, fw, dw, gw}
	case w >= 80:
		fw := clampInt(w/4, 22, 26) // compact sidebar (tree names only)
		dw := w - fw - panesChrome(2)
		return layoutResult{layoutMedium, fw, dw, 0}
	default:
		return layoutResult{layoutNarrow, 0, 0, 0}
	}
}

// findingsWidth is the inner width for the findings/comments navigator
// content (right pane on wide, full width on narrow, drawer on medium).
func (m *model) findingsWidth() int {
	L := m.layout()
	switch L.mode {
	case layoutNarrow:
		return m.width - 2
	case layoutMedium:
		return clampInt(m.width*2/5, 28, 48) // drawer width
	default:
		return L.findingsW
	}
}

// findingsDrawerWidth is the drawer box width on medium terminals.
func findingsDrawerWidth(w int) int { return clampInt(w*2/5, 28, 48) }

// paneHeader renders the focus-aware one-line pane title.
func (m *model) paneHeader(title string, focused bool) string {
	if focused {
		return styleBorderF.Render(focusIndicator(true) + " " + title)
	}
	return styleDim.Render(focusIndicator(false) + " " + title)
}

// renderPaneBox wraps pane rows into a full bordered box of inner width pw
// and bodyHeight rows total. Focused panes get styleBorderF.
func (m *model) renderPaneBox(rows []string, pw int, focused bool) string {
	b := styleBorder
	if focused {
		b = styleBorderF
	}
	inner := m.bodyHeight() - 2
	out := make([]string, 0, inner+2)
	out = append(out, b.Render("╭"+strings.Repeat("─", pw)+"╮"))
	for i := 0; i < inner; i++ {
		var l string
		if i < len(rows) {
			l = cut(rows[i], pw)
		}
		out = append(out, b.Render("│"+pad(l, pw)+"│"))
	}
	out = append(out, b.Render("╰"+strings.Repeat("─", pw)+"╯"))
	return strings.Join(out, "\n")
}

// renderFilesPane renders the left FILES pane at the width it is given
// (sidebar in the 3-pane layout, full width when zoomed).
func (m *model) renderFilesPane(w, h int) []string {
	return m.filesPaneRows(w, h)
}

// renderDiffPane renders the center DIFF pane: the continuous all-MR diff
// (the pane header carries the focus dot; browseDiffContent's title row
// follows). The browseFile fallback positions the document WITHOUT side
// effects: browseFilePath() would steal focus to the diff pane mid-render
// and break focus-gated overlays (e.g. the medium findings drawer).
func (m *model) renderDiffPane(w, h int) []string {
	if !m.browseSeeded && m.browseFile == "" {
		if files := m.changedFiles(); len(files) > 0 {
			p := files[0]
			m.ensureDiff()
			if m.diffParsed[p] != nil {
				m.browseSeeded = true // one-shot: never yank mid-session
				m.diffCursor = m.firstAddressableRow(p)
				m.syncBrowseFile()
				m.ensureBrowseVisible()
			}
		}
	}
	out := []string{m.paneHeader("DIFF", m.focus == focusDiff)}
	return append(out, m.browseDiffContent(w)...)
}

// renderFindingsPane renders the right pane skeleton: FINDINGS header,
// the findings card list, a separator, then the SELECTED FINDING details
// region (no tabs on wide screens).
func (m *model) renderFindingsPane(w, h int) []string {
	if m.leftView == viewComments {
		out := []string{m.paneHeader("COMMENTS", m.focus == focusFindings)}
		return append(out, m.commentsView()...)
	}
	// the findings list header IS the pane header: it carries the focus
	// indicator + visible/total + pending count (findingsListHeader)
	out := make([]string, 0, h)
	listH := h * 45 / 100
	if listH < 4 {
		listH = 4
	}
	list := m.findingsView()
	for i := 0; i < listH; i++ {
		if i < len(list) {
			out = append(out, list[i])
		} else {
			out = append(out, "")
		}
	}
	out = append(out, styleBorder.Render(strings.Repeat("─", max(4, w-2))))
	out = append(out, styleSection.Render("SELECTED FINDING"))
	if f := m.current(); f != nil {
		out = append(out, m.overviewSections(f, w, false)...)
	} else {
		out = append(out, styleDim.Render("no finding selected — j/k in the findings list"))
	}
	if len(out) > h {
		out = out[:h] // the pane interior is exactly h rows — never overflow
	}
	return out
}

// navigatorPaneRows renders the narrow-mode single pane for navigator focus.
func (m *model) navigatorPaneRows(h int) []string { return m.listViewBody() }

// startDiffFirst seeds the startup state: the continuous diff visible and
// focused in the center pane (first changed file, cursor at its first row).
func (m *model) startDiffFirst() {
	m.focus = focusDiff
	if m.browseFile == "" {
		if files := m.changedFiles(); len(files) > 0 {
			m.browseFilePath(files[0])
		}
	}
	m.ensureBrowseVisible()
}
