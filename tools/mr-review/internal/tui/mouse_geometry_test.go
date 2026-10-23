package tui

// Regression tests for the mouse findings from the redesign review:
// hitTest must ignore the zoomed layout, and the wheel must always scroll
// the pane under the pointer (never whatever pane holds focus).

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestHitTestIgnoresZoom(t *testing.T) {
	m := filesModel(t, 120, 35)
	m.focus = focusDiff
	m.zoom = true
	if _, _, _, ok := m.hitTest(5, 10); ok {
		t.Fatal("hitTest must report no pane while zoomed (single full-width pane)")
	}
	// a click while zoomed must not change focus or view
	before, beforeView := m.focus, m.leftView
	m.mouseClick(tea.Mouse{X: 5, Y: 10, Button: tea.MouseLeft})
	if m.focus != before || m.leftView != beforeView {
		t.Fatal("click while zoomed must not change pane focus/view")
	}
}

func TestMouseWheelDiffAfterBrowseExit(t *testing.T) {
	m := filesModel(t, 120, 35)
	m.buildTree()
	m.focus = focusDiff
	m.browseFilePath("services/x/handler.go")
	top := m.detailTop[tabOverview]
	m.exitBrowse() // browseFile == "" now
	if m.browseFile != "" {
		t.Fatal("precondition: browse must be exited")
	}
	out, _ := m.Update(tea.MouseWheelMsg{X: 60, Y: 10, Button: tea.MouseWheelDown}) // over the diff pane
	mm := out.(*model)
	if mm.focus != focusDiff {
		t.Fatalf("wheel over the diff pane must focus the diff pane, got %d", mm.focus)
	}
	// scrolling must act on the diff scroll offset, not the findings cursor
	if mm.detailTop[tabOverview] == top && mm.cursor != 0 {
		t.Fatal("wheel over an exited diff must scroll the diff, not the findings list")
	}
}
