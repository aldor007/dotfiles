package widget

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// TabBarStyles carries the theme-resolved styles a tab bar needs.
type TabBarStyles struct {
	Border      lipgloss.Style // unfocused borders/separators
	BorderFocus lipgloss.Style // focused borders/separators
	Dim         lipgloss.Style // inactive tab labels
	Active      lipgloss.Style // active tab segment (bold + colored bg)
	Cursor      lipgloss.Style // plain-mode active segment (reverse)
}

// TabBarWidth computes the natural width of a tab bar with the given names
// (callers use it to decide between full and short label sets).
func TabBarWidth(names []string) int {
	const tabPad = 2
	total := 1
	for _, n := range names {
		total += lipgloss.Width(n) + tabPad*2 + 1
	}
	return total
}

// RenderTabBar renders the boxed, segmented tab bar (3 rows). The bar always
// spans exactly w cells: the last segment stretches to fill the remaining
// width, so the bar connects to the pane's right edge and its bottom border
// doubles as the separator above the content. A very narrow bar drops the
// last label to "…". Byte-compatible with the legacy tabBarView.
func RenderTabBar(names []string, active int, focused bool, plain bool, w int, s TabBarStyles) []string {
	const tabPad = 2
	widths := make([]int, len(names))
	total := 1
	for i, n := range names {
		widths[i] = lipgloss.Width(n) + tabPad*2
		total += widths[i] + 1
	}
	for total > w && len(widths) > 1 {
		// drop the last tab label into "…" (rare, very narrow)
		widths[len(widths)-1] = 5
		names = append([]string(nil), names...)
		names[len(names)-1] = "…"
		total = 1
		for _, wd := range widths {
			total += wd + 1
		}
		break
	}
	// stretch the last segment so the bar spans the full pane width
	if extra := w - total; extra > 0 {
		widths[len(widths)-1] += extra
	}

	border := s.Border
	if focused {
		border = s.BorderFocus
	}
	sep := border.Render("┬")
	cornerTR, cornerBR := border.Render("┐"), border.Render("┘")
	top := "┌"
	labels := "│"
	bottom := "└"
	for i, n := range names {
		wd := widths[i]
		styled := s.Dim.Render(padTo(n, wd))
		if i == active {
			if plain {
				styled = s.Cursor.Render(padTo("["+n+"]", wd))
			} else {
				styled = s.Active.Render(padTo(n, wd))
			}
		}
		bar := strings.Repeat("─", wd)
		top += bar + sep
		labels += styled + "│"
		bottom += bar + sep
		if i == len(names)-1 {
			// last segment: corner pieces
			top = strings.TrimSuffix(top, sep) + cornerTR
			bottom = strings.TrimSuffix(bottom, sep) + cornerBR
		}
	}
	return []string{top, labels, bottom}
}

func padTo(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}
