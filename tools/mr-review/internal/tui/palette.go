// palette.go — the vi-SQL-inspired command palette. A modal, fuzzy-filtered
// list over the unified action registry: every entry is the same action the
// keyboard dispatches, so the palette can never drift from the real handlers.
//
// Safety: the palette executes through Action.Run — publish still goes
// through the preview + typed confirmation, never straight to GitLab.
package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// openPalette activates the palette (registry action view.palette). The
// palette is a stack member: it pushes ovPalette so Esc pops exactly one
// level (a nested overlay closes back into the palette).
func (m *model) openPalette() tea.Cmd {
	m.paletteOn = true
	m.paletteSel = 0
	m.paletteInput.SetValue("")
	m.paletteInput.Focus()
	m.refreshPalette()
	m.pushOverlay(ovPalette)
	return textinput.Blink
}

func (m *model) closePalette() {
	m.paletteOn = false
	m.paletteInput.Blur()
	m.popOverlay() // pop the palette level; nested overlays return here
	// previous focus is preserved: the palette never changes m.focus
}

// refreshPalette refilters the action list for the current query.
func (m *model) refreshPalette() {
	all := enabledActions(m)
	q := strings.ToLower(strings.TrimSpace(m.paletteInput.Value()))
	m.paletteItems = m.paletteItems[:0]
	for _, a := range all {
		if q == "" || fuzzyScore(strings.ToLower(a.Name+" "+a.Desc), q) >= 0 {
			m.paletteItems = append(m.paletteItems, a)
		}
	}
	if m.paletteSel >= len(m.paletteItems) {
		m.paletteSel = len(m.paletteItems) - 1
	}
	if m.paletteSel < 0 {
		m.paletteSel = 0
	}
}

func (m *model) updatePalette(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closePalette()
		return m, nil
	case "down", "ctrl+n":
		if m.paletteSel < len(m.paletteItems)-1 {
			m.paletteSel++
		}
		return m, nil
	case "up", "ctrl+p":
		if m.paletteSel > 0 {
			m.paletteSel--
		}
		return m, nil
	case "enter":
		if m.paletteSel < 0 || m.paletteSel >= len(m.paletteItems) {
			m.closePalette()
			return m, nil
		}
		a := m.paletteItems[m.paletteSel]
		m.closePalette()
		return m, m.run(&a)
	}
	var cmd tea.Cmd
	m.paletteInput, cmd = m.paletteInput.Update(msg)
	m.refreshPalette()
	return m, cmd
}

// paletteView renders the overlay (bordered box, centered).
func (m *model) paletteView(w, h int) string {
	const maxRows = 12
	boxW := min(w-6, 92)
	boxH := min(h-4, maxRows+4)

	var b strings.Builder
	b.WriteString(styleHeader.Render("ACTIONS") + "\n")
	b.WriteString(styleBorder.Render(strings.Repeat("─", boxW-2)) + "\n")
	if len(m.paletteItems) == 0 {
		b.WriteString(styleDim.Render(" no matching actions") + "\n")
	}
	rows := boxH - 4 // title + rule + input + hint
	top := 0
	if m.paletteSel >= rows {
		top = m.paletteSel - rows + 1
	}
	for i := top; i < len(m.paletteItems) && i < top+rows; i++ {
		a := m.paletteItems[i]
		line := fmt.Sprintf("  %-32s %-38s %s",
			truncPlain(a.Name, 32), truncPlain(a.Desc, 38), a.KeyLabel())
		if plainMode() {
			if i == m.paletteSel {
				line = ">" + line[1:]
			}
		} else if i == m.paletteSel {
			line = styleCursor.Render(pad(line, boxW-4))
		}
		b.WriteString(cut(line, boxW-2) + "\n")
	}
	b.WriteString(styleBorder.Render(strings.Repeat("─", boxW-2)) + "\n")
	b.WriteString(m.paletteInput.View() + "\n")
	b.WriteString(styleDim.Render(" type to filter · ↑/↓ or ctrl+n/p · enter run · esc cancel") + "\n")
	return overlayBoxRaw(b.String(), boxW, boxH)
}

// overlayBoxRaw draws a box around pre-rendered content.
func overlayBoxRaw(content string, w, h int) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) > h-2 {
		lines = lines[:h-2]
	}
	for len(lines) < h-2 {
		lines = append(lines, "")
	}
	var out strings.Builder
	out.WriteString("┌" + strings.Repeat("─", w-2) + "┐\n")
	for _, l := range lines {
		out.WriteString("│" + pad(cut(l, w-2), w-2) + "│\n")
	}
	out.WriteString("└" + strings.Repeat("─", w-2) + "┘\n")
	return out.String()
}

// fuzzyScore returns a match score (>=0 match, <0 no match). Plain-subsequence
// with bonuses for word starts and consecutive hits — small and predictable.
func fuzzyScore(hay, needle string) int {
	if needle == "" {
		return 0
	}
	score := 0
	hi := 0
	consecutive := 0
	for i := 0; i < len(needle); i++ {
		idx := strings.IndexByte(hay[hi:], needle[i])
		if idx < 0 {
			return -1
		}
		idx += hi
		if idx == 0 || hay[idx-1] == ' ' || hay[idx-1] == ':' {
			score += 3 // word start
		}
		if idx == hi && i > 0 {
			consecutive++
			score += 2
		} else {
			consecutive = 0
		}
		score += 1
		hi = idx + 1
	}
	// the length penalty only RANKS tighter matches — a matched subsequence
	// must never go non-positive (a single-letter query used to score -3 on
	// long names and the palette showed nothing at all)
	if s := score - (len(hay)-len(needle))/8; s > 0 {
		return s
	}
	return 0
}

// ensure lipgloss import is used (palette rows measure nothing today, but the
// styles above come from the theme).
var _ = lipgloss.Width
