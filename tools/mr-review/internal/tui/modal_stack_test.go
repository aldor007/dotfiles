package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestOverlayStackNestingPaletteFilter demonstrates the acceptance case for
// the focus stack: palette → filter (pushed on top) → Esc returns to the
// palette → Esc again closes it. Today's real flows never nest, but the
// stack must support it (vi-SQL peeker fix #65).
func TestOverlayStackNestingPaletteFilter(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	m.openPalette()
	if m.overlay != ovPalette || m.topOverlay() != ovPalette {
		t.Fatalf("palette must be a stack member, overlay=%d top=%d", m.overlay, m.topOverlay())
	}
	// a nested overlay opens on top of the palette
	m.pushOverlay(ovFilter)
	if m.overlay != ovFilter || m.topOverlay() != ovFilter {
		t.Fatalf("nested filter must become the top, overlay=%d top=%d", m.overlay, m.topOverlay())
	}
	// Esc pops one level only: back to the palette
	m.popOverlay()
	if m.overlay != ovPalette || m.topOverlay() != ovPalette {
		t.Fatalf("Esc must return to the palette, overlay=%d top=%d", m.overlay, m.topOverlay())
	}
	if !m.paletteOn {
		t.Fatal("palette must still be open underneath")
	}
	// Esc again closes the palette entirely: the palette's own close path
	// pops the stack level and clears paletteOn.
	m.closePalette()
	if m.overlay != ovNone || m.topOverlay() != ovNone || m.paletteOn {
		t.Fatalf("second Esc must close the palette, overlay=%d paletteOn=%v", m.overlay, m.paletteOn)
	}
}

// TestOverlayStackMirrorsField — pushOverlay/popOverlay keep the plain
// overlay field in sync with the stack top (empty stack ⇒ ovNone).
func TestOverlayStackMirrorsField(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	if len(m.overlayStack) != 0 {
		t.Fatalf("fresh model must start with an empty stack, got %d", len(m.overlayStack))
	}
	m.pushOverlay(ovSummary)
	m.pushOverlay(ovHelp)
	m.pushOverlay(ovPublish)
	if m.overlay != ovPublish || len(m.overlayStack) != 3 {
		t.Fatalf("overlay must mirror the stack top, overlay=%d stack=%d", m.overlay, len(m.overlayStack))
	}
	m.popOverlay()
	if m.overlay != ovHelp {
		t.Fatalf("pop must restore the previous top, overlay=%d", m.overlay)
	}
	m.popOverlay()
	m.popOverlay()
	if m.overlay != ovNone || len(m.overlayStack) != 0 {
		t.Fatalf("empty stack must read ovNone, overlay=%d stack=%d", m.overlay, len(m.overlayStack))
	}
	// popping an already-empty stack stays ovNone (harmless)
	m.popOverlay()
	if m.overlay != ovNone {
		t.Fatalf("pop on empty stack must stay ovNone, overlay=%d", m.overlay)
	}
}

// TestOverlayDirectAssignmentClosesViaStack — the tests and the (untouched)
// registry assign m.overlay directly with an empty stack; the pop paths must
// still close to ovNone exactly as before.
func TestOverlayDirectAssignmentClosesViaStack(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	m.overlay = ovInspector // as layout_test.go / manual_test.go do
	if m.topOverlay() != ovInspector {
		t.Fatalf("direct assignment must be the top with an empty stack, top=%d", m.topOverlay())
	}
	// esc via updateOverlayKey pops one level
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m2.(*model).overlay != ovNone {
		t.Fatalf("esc must close a directly-assigned overlay, overlay=%d", m2.(*model).overlay)
	}
}

// TestPublishPreviewStackFlow — the safety-critical publish preview opens
// and closes through the stack with unchanged semantics: P → preview (top),
// Esc cancels (pop → ovNone), P again reopens.
func TestPublishPreviewStackFlow(t *testing.T) {
	fs := mockFindings(2)
	fs[0].Decision = "approved"
	m := newTestModel(t, 120, 35, fs)
	m2, _ := m.Update(key("P"))
	mm := m2.(*model)
	if mm.overlay != ovPublish || mm.topOverlay() != ovPublish {
		t.Fatalf("P must push the preview, overlay=%d top=%d", mm.overlay, mm.topOverlay())
	}
	if v := mm.View().Content; !strings.Contains(v, "PUBLICATION PREVIEW") {
		t.Fatal("preview overlay not rendered via the stack top")
	}
	m3, _ := mm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m3.(*model).overlay != ovNone {
		t.Fatalf("Esc must pop the preview, overlay=%d", m3.(*model).overlay)
	}
	m4, _ := m3.Update(key("P"))
	if m4.(*model).overlay != ovPublish {
		t.Fatalf("P must re-open the preview, overlay=%d", m4.(*model).overlay)
	}
}
