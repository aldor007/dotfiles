// inspector.go — the finding inspector (vi-SQL peek pattern) and the draft
// editor overlay rendering. Both are modal screens that preserve all
// navigation state underneath; closing restores the exact previous context.
package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// inspectorView renders the quick finding inspection overlay.
func (m *model) inspectorView(w, h int) string {
	f := m.current()
	if f == nil {
		return overlayBoxRaw(styleDim.Render("no finding selected")+"\n", min(w-6, 80), min(h-4, 8))
	}
	boxW := min(w-6, 96)
	boxH := min(h-4, 22)

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s — %s\n", sevChip(f), styleSel.Render(f.ID), styleHeader.Render(truncPlain(f.Title, boxW-14)))
	fmt.Fprintf(&b, "file: %s · side: %s · kind: %s · confidence: %s\n",
		smartPath(f.File, boxW-40), f.Side, f.Kind, f.Confidence)
	loc := fmt.Sprintf("%d", f.Line)
	if f.LineEnd > f.Line {
		loc += fmt.Sprintf("-%d", f.LineEnd)
	}
	fmt.Fprintf(&b, "state: %s · location: :%s\n", decisionStyle(f).Render(strings.ToUpper(decisionLabel(f))), loc)
	b.WriteString(styleBorder.Render(strings.Repeat("─", boxW-2)) + "\n")
	for _, l := range wrap(f.Explanation, boxW-4) {
		b.WriteString(cut(l, boxW-2) + "\n")
	}
	if strings.TrimSpace(f.Evidence) != "" {
		b.WriteString(styleSection.Render("evidence") + "\n")
		for _, l := range wrap(f.Evidence, boxW-4) {
			b.WriteString(styleDim.Render(cut(l, boxW-2)) + "\n")
		}
	}
	if c := f.EffectiveComment(); strings.TrimSpace(c) != "" {
		b.WriteString(styleSection.Render("suggested comment") + "\n")
		for _, l := range wrap(firstLine(c), boxW-4) {
			b.WriteString(cut(l, boxW-2) + "\n")
		}
		if strings.Count(c, "\n") > 0 {
			b.WriteString(styleDim.Render("  …full text in the Comment tab") + "\n")
		}
	}
	b.WriteString(styleBorder.Render(strings.Repeat("─", boxW-2)) + "\n")
	b.WriteString(styleDim.Render("a approve · r reject · e edit · d discuss · S stage in tuicr · other key closes") + "\n")
	return overlayBoxRaw(b.String(), boxW, boxH)
}

// draftEditorView renders the manual comment editor overlay.
func (m *model) draftEditorView(w, h int) string {
	e := m.draftEdit
	boxW := min(w-6, 92)
	boxH := min(h-4, 18)
	var b strings.Builder
	discard := ""
	if e.armDiscard {
		discard = styleWarn.Render("  UNSAVED — esc discards")
	}
	b.WriteString(styleHeader.Render(e.title()) + " " + discard + "\n")
	b.WriteString(styleBorder.Render(strings.Repeat("─", boxW-2)) + "\n")
	ta := e.ta.View()
	for _, l := range strings.Split(ta, "\n") {
		b.WriteString(cut(l, boxW-2) + "\n")
	}
	b.WriteString(styleBorder.Render(strings.Repeat("─", boxW-2)) + "\n")
	b.WriteString(styleDim.Render("ctrl+s save draft (local only) · esc cancel · markdown supported") + "\n")
	return overlayBoxRaw(b.String(), boxW, boxH)
}

// ensure lipgloss usage (styles come from the theme).
var _ = lipgloss.Width
