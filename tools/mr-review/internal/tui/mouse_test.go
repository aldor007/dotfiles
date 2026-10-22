package tui

// Mouse e2e: click positions, drag selects, release copies — the cockpit
// analogue of the y key, driven through the real Update path with synthetic
// terminal mouse events (coordinates zero-based, see tea.Mouse).

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMouseCopyInDiff(t *testing.T) {
	dir := writeReviewFixture(t, t.TempDir())
	m := newSessionModel(t, dir)
	m = press(m, "g", "d") // continuous diff
	lw := m.listWidth()    // left pane width; right pane starts at lw+2

	// click a diff content row: right pane, first content row under the tab bar
	out, _ := m.Update(tea.MouseClickMsg{X: lw + 4, Y: 3 + 4 + 2, Button: tea.MouseLeft})
	m = out.(*model)
	if m.diffCursor == 0 {
		t.Fatal("click must move the diff cursor")
	}
	if m.visualOn {
		t.Error("plain click must clear/start a fresh selection")
	}

	// drag two rows down: motion with the left button held
	out, _ = m.Update(tea.MouseMotionMsg{X: lw + 4, Y: 3 + 4 + 4, Button: tea.MouseLeft})
	m = out.(*model)
	lo, hi := m.visualRange()
	if !m.visualOn || hi-lo != 2 {
		t.Fatalf("drag must select 3 rows, visualOn=%v range=[%d..%d]", m.visualOn, lo, hi)
	}

	// release: copies the selection (OSC52 via the program), highlight stays
	out, _ = m.Update(tea.MouseReleaseMsg{X: lw + 4, Y: 3 + 4 + 4, Button: tea.MouseLeft})
	m = out.(*model)
	if !strings.Contains(m.msg, "copied 3 line(s) via mouse") {
		t.Fatalf("release must copy the selection, msg = %q", m.msg)
	}
	if !m.visualOn {
		t.Error("selection highlight should stay visible after copy")
	}

	// a fresh click clears the stale selection
	out, _ = m.Update(tea.MouseClickMsg{X: lw + 4, Y: 3 + 4 + 2, Button: tea.MouseLeft})
	m = out.(*model)
	if m.visualOn {
		t.Error("new click must clear the previous selection")
	}
}

func TestMouseWheelScrolls(t *testing.T) {
	dir := writeReviewFixture(t, t.TempDir())
	m := newSessionModel(t, dir)
	before := m.cursor
	out, _ := m.Update(tea.MouseWheelMsg{X: 5, Y: 10, Button: tea.MouseWheelDown})
	m = out.(*model)
	if m.cursor <= before {
		t.Fatalf("wheel down must move the findings cursor: %d → %d", before, m.cursor)
	}
	out, _ = m.Update(tea.MouseWheelMsg{X: 5, Y: 10, Button: tea.MouseWheelUp})
	m = out.(*model)
	if m.cursor != before {
		t.Fatalf("wheel up must return the cursor to %d, got %d", before, m.cursor)
	}
}

func TestMouseIgnoredInTextModes(t *testing.T) {
	dir := writeReviewFixture(t, t.TempDir())
	m := newSessionModel(t, dir)
	m = press(m, "g", "d")
	m = press(m, "/") // search input open
	m = press(m, "x") // typed into the search, not a shortcut
	if !m.searchOn {
		t.Fatal("search must stay open")
	}
	out, _ := m.Update(tea.MouseClickMsg{X: 60, Y: 10, Button: tea.MouseLeft})
	m = out.(*model)
	if !m.searchOn {
		t.Error("mouse must not dismiss/act while a text mode is active")
	}
}
