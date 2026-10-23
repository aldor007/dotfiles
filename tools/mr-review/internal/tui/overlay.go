// overlay.go — floating overlay compositing (the OverlayManager).
//
// Overlays never replace the body: View() renders the normal frame
// (header + body + footer) exactly as it would with no overlay open, then
// composites the overlay's bordered box OVER those lines. Backdrop rows
// outside the box are dimmed with SGR faint; rows under the box keep only
// the box cells (cell-exact replacement, ANSI-aware via skipCells).
package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// overlayOpts controls compositeOverlay placement/degradation.
type overlayOpts struct {
	width       int  // viewport width in cells
	anchorRight bool // anchor the box to the right edge (medium findings drawer)
	anchorLow   bool // anchor the box to the bottom edge (comment editor: keeps
	// the diff anchor line visible above the editor)
	plain bool // plainMode(): skip dim codes (NO_COLOR safety)
}

// compositeOverlay places the bordered box lines over the base frame lines
// and returns the composed frame. The box is centered horizontally and
// vertically, clamped to a 1-cell margin. Only the overlapped cell region of
// each row is replaced; the rest of the row (including trailing borders) is
// preserved. Styled base lines are safe: skipCells keeps escape sequences
// intact and the dim wrapper always resets.
func compositeOverlay(base []string, box []string, opts overlayOpts) string {
	if len(base) == 0 {
		return ""
	}
	if len(box) == 0 {
		return strings.Join(base, "\n")
	}
	boxW := 0
	for _, l := range box {
		if w := lipgloss.Width(l); w > boxW {
			boxW = w
		}
	}
	rows := make([]string, len(box))
	for i, l := range box {
		rows[i] = pad(l, boxW) // uniform width → contiguous replacement region
	}
	x := (opts.width - boxW) / 2
	if opts.anchorRight {
		x = opts.width - boxW - 1
	}
	if x < 1 {
		x = 1
	}
	if x+boxW > opts.width-1 {
		x = max(0, opts.width-1-boxW)
	}
	if x+boxW > opts.width { // a full-width box must still fit the line
		x = max(0, opts.width-boxW)
	}
	y := (len(base) - len(rows)) / 2
	if opts.anchorLow {
		y = len(base) - 1 - len(rows) // bottom edge: leave a 1-row margin
	}
	if y < 1 {
		y = 1
	}
	if y+len(rows) > len(base)-1 {
		y = max(1, len(base)-1-len(rows))
	}
	out := make([]string, len(base))
	for i, bl := range base {
		switch {
		case i < y || i >= y+len(rows) || i >= len(base):
			out[i] = dimSegment(bl, opts.plain)
		default:
			line := pad(cut(bl, opts.width), opts.width)
			left := cut(line, x) // first x cells (skipCells drops them instead)
			right := skipCells(line, x+boxW)
			out[i] = dimSegment(left, opts.plain) + rows[i-y] + dimSegment(right, opts.plain)
		}
	}
	return strings.Join(out, "\n")
}

// dimSegment wraps a backdrop fragment in SGR faint (2) with a hard reset.
// Existing style runs inside the fragment survive; the trailing reset never
// leaks faint into the overlay box.
func dimSegment(s string, plain bool) string {
	if plain || s == "" {
		return s
	}
	return "\x1b[2m" + s + "\x1b[0m"
}
