package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Terminal query responses (OSC 11 background color, DA1, …) can leak into
// the input stream as rune key events when a query races Bubble Tea's
// reader. Typed text never contains control or C1 bytes, so the key router
// must drop such events entirely — they previously typed themselves into
// the palette filter ("0 actions" with `rgb:0000/0000/0000` in it).
func TestSanitizeKeyEvents(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.openPalette()
	before := len(m.paletteItems)
	if before == 0 {
		t.Fatal("palette must list actions after open")
	}

	leaked := tea.KeyPressMsg{Text: "\x1b]11;rgb:0000/0000/0000\x1b\\"}
	m.Update(leaked)
	if len(m.paletteItems) != before {
		t.Fatalf("leaked OSC response mutated the palette: %d items, want %d", len(m.paletteItems), before)
	}
	if got := m.paletteInput.Value(); got != "" {
		t.Fatalf("leaked OSC response typed into the filter: %q", got)
	}

	// printable text still flows through
	ok := tea.KeyPressMsg{Text: "a"}
	mm, _ := m.Update(ok)
	m2 := mm.(*model)
	if m2.paletteInput.Value() != "a" {
		t.Fatalf("printable key dropped: %q", m2.paletteInput.Value())
	}
	if len(m2.paletteItems) == 0 {
		t.Fatal("filtering by 'a' must still match actions")
	}
}

// mdStyleName must never depend on a terminal query (the glamour auto-style
// OSC 11 race) — it derives from NO_COLOR and the active theme instead.
func TestMdStyleName(t *testing.T) {
	t.Cleanup(func() { _ = SetTheme("default", nil) })
	t.Setenv("NO_COLOR", "1")
	if got := mdStyleName(); got != "ascii" {
		t.Errorf("NO_COLOR: mdStyleName = %q, want ascii", got)
	}
	t.Setenv("NO_COLOR", "")
	if err := SetTheme("dracula", nil); err != nil {
		t.Fatalf("SetTheme dracula: %v", err)
	}
	if got := mdStyleName(); got != "dracula" {
		t.Errorf("dracula theme: mdStyleName = %q, want dracula", got)
	}
	if err := SetTheme("default", nil); err != nil {
		t.Fatalf("SetTheme default: %v", err)
	}
	if got := mdStyleName(); got != "dark" {
		t.Errorf("default theme: mdStyleName = %q, want dark", got)
	}
}
