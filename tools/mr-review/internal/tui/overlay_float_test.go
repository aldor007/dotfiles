package tui

// Floating-overlay contract tests (redesign rules 4/5). Overlays must FLOAT
// over the unchanged frame: rows outside the overlay box keep their exact
// content (only SGR-dimmed, which strips away), the box is centered with a
// 1-cell margin, and the medium findings drawer is right-anchored.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// overlayBoxGeometry mirrors compositeOverlay's vertical placement contract:
// centered, clamped to a 1-cell margin.
func overlayBoxGeometry(baseLen, boxLen int) int {
	y := (baseLen - boxLen) / 2
	if y < 1 {
		y = 1
	}
	if y+boxLen > baseLen-1 {
		y = max(1, baseLen-1-boxLen)
	}
	return y
}

// overlayBoxX mirrors compositeOverlay's horizontal placement contract.
func overlayBoxX(boxW, width int) int {
	x := (width - boxW) / 2
	if x < 1 {
		x = 1
	}
	if x+boxW > width-1 {
		x = max(0, width-1-boxW)
	}
	if x+boxW > width {
		x = max(0, width-boxW)
	}
	return x
}

// TestHelpOverlayFloatsIntactBody — the BODY under a floating overlay keeps
// its exact ANSI-stripped content on every row outside the box (the SGR dim
// codes are intentional and must be the ONLY difference). Header rows must
// match too; the two FOOTER rows are excluded: the mode label intentionally
// reflects the overlay state (e.g. HELP) while an overlay is open.
func TestHelpOverlayFloatsIntactBody(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	base := strings.Split(m.normalFrame(), "\n")
	m.pushOverlay(ovHelp)
	box, _ := m.activeOverlayBox()
	composed := strings.Split(m.View().Content, "\n")
	if len(composed) != len(base) {
		t.Fatalf("overlay changed the frame height: %d → %d", len(base), len(composed))
	}
	y := overlayBoxGeometry(len(base), len(box))
	bodyEnd := len(base) - 2 // exclude the 2 footer rows (mode label)
	for i := 0; i < len(base); i++ {
		if i >= bodyEnd {
			break
		}
		if i < y || i >= y+len(box) {
			if stripANSIish(composed[i]) != stripANSIish(base[i]) {
				t.Fatalf("row %d outside the overlay box was reflowed:\nbase: %q\ngot:  %q",
					i, stripANSIish(base[i]), stripANSIish(composed[i]))
			}
		}
	}
	m.popOverlay()
}

// cellIndexOfRunes returns the CELL index of r in s (s must already be
// ANSI-stripped; every remaining rune is one cell wide). Byte-based
// strings.Index overcounts multi-byte runes (box drawing, ○, ●).
func cellIndexOfRunes(s string, r rune) int {
	n := 0
	for _, c := range s {
		if c == r {
			return n
		}
		n++
	}
	return -1
}

// TestOverlayBoxCentered — the help overlay box is horizontally centered at
// both a small (80x24) and a large (200x50) terminal, within the margins.
func TestOverlayBoxCentered(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {200, 50}} {
		m := newMultiFileModel(t, sz[0], sz[1])
		_ = m.View().Content // warm-up: settle render-time state
		m.pushOverlay(ovHelp)
		box, _ := m.activeOverlayBox()
		composed := strings.Split(m.View().Content, "\n")
		boxW := lipgloss.Width(box[0])
		if boxW > m.width-2 {
			t.Fatalf("%dx%d: overlay box %d cells leaves no 1-cell margin", sz[0], sz[1], boxW)
		}
		y := overlayBoxGeometry(len(composed), len(box))
		x := overlayBoxX(boxW, m.width)
		top := stripANSIish(composed[y])
		if !strings.Contains(top, "┌") {
			t.Fatalf("%dx%d: overlay box top border missing at row %d: %q", sz[0], sz[1], y, top)
		}
		if got := cellIndexOfRunes(top, '┌'); got != x {
			t.Fatalf("%dx%d: box not centered: left margin %d, want %d (boxW=%d)", sz[0], sz[1], got, x, boxW)
		}
		m.popOverlay()
	}
}

// TestMediumFindingsDrawer — at 80 cols (medium) focusing findings floats a
// RIGHT-ANCHORED drawer over the body; any other focus hides it.
func TestMediumFindingsDrawer(t *testing.T) {
	m := newMultiFileModel(t, 80, 24)
	press(m, "3") // focus findings → the drawer overlay opens
	v := m.View().Content
	plain := stripANSIish(v)
	if !strings.Contains(plain, "SELECTED FINDING") {
		t.Fatal("medium findings drawer missing")
	}
	box, opts := m.activeOverlayBox()
	if !opts.anchorRight {
		t.Fatal("medium findings drawer must be right-anchored")
	}
	boxW := lipgloss.Width(box[0])
	composed := strings.Split(v, "\n")
	y := overlayBoxGeometry(len(composed), len(box))
	wantX := m.width - boxW - 1
	row := []rune(stripANSIish(composed[y]))
	if wantX < 0 || wantX >= len(row) || row[wantX] != '╭' {
		t.Fatalf("drawer not right-anchored: expected ╭ at column %d (boxW=%d), row=%q",
			wantX, boxW, string(row))
	}
	// focus elsewhere → the drawer disappears
	press(m, "2")
	if after := stripANSIish(m.View().Content); strings.Contains(after, "SELECTED FINDING") {
		t.Fatal("findings drawer must hide when findings lose focus")
	}
}

// TestNarrowSinglePaneLayout — below 80 cols exactly ONE pane renders and it
// fills the terminal width.
func TestNarrowSinglePaneLayout(t *testing.T) {
	m := newTestModel(t, 70, 24, mockFindings(5))
	if m.layout().mode != layoutNarrow {
		t.Fatal("70 cols must be narrow mode")
	}
	plain := stripANSIish(m.View().Content)
	if tops := strings.Count(plain, "╭"); tops != 1 {
		t.Fatalf("narrow mode must render exactly one pane, got %d box tops", tops)
	}
	for _, l := range strings.Split(plain, "\n") {
		if strings.HasPrefix(l, "╭") && lipgloss.Width(l) != m.width {
			t.Fatalf("narrow pane must fill the terminal width: %d != %d", lipgloss.Width(l), m.width)
		}
	}
	_ = tea.KeyEsc // keep the tea import if assertions above change
}
