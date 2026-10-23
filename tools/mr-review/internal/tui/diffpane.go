package tui

import (
	"fmt"

	"mr-review/internal/store"

	"charm.land/lipgloss/v2"
)

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
		texts[i] = expandTabs(r.Text)
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
	top := m.detailTop[tabOverview]
	if top > len(all)-h {
		top = max(0, len(all)-h)
	}
	if top < 0 {
		top = 0
	}
	m.detailTop[tabOverview] = top
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
