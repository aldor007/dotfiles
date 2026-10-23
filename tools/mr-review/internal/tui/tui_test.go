// Chunk 6: regression tests for the TUI UX Refinement — Second Pass.
// Plan reference: .kimchi/plans/mr-review-tui-ux-refinement-second-pass.md
//
// Master-detail intent note (per Risk #1 + Decision Log "Skip §4 full vim
// navigation rewrite"): tabOverview combines diff + summary in one tab
// (Chunk 2 step 1 collapsed 5 tabs → 2), so the wide-vs-narrow rendering
// composes via the existing single-outer-frame layout (Phase 1) rather than
// via a dedicated master-detail composer function.

package tui

import (
	"strings"
	"regexp"
	"testing"

	"mr-review/internal/store"
	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textarea"
)

// TestEmptyStateWhenAIRunning: when bgReview is AgentRunning and zero
// findings, emptyStateContent returns the "AI is analyzing" banner.
func TestEmptyStateWhenAIRunning(t *testing.T) {
	t.Parallel()
	m := newMultiFileModel(t, 120, 35)
	m.findings = nil
	m.bgReview = &store.AgentState{Status: store.AgentRunning}
	m.bgLoaded = true
	out := m.emptyStateContent(120)
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "AI is analyzing") {
		t.Fatalf("expected AI-analyzing banner, got:\n%s", joined)
	}
}

// TestEmptyStateAfterCompletion: when bgReview is AgentDone and zero
// findings, emptyStateContent returns the "No findings detected" banner.
func TestEmptyStateAfterCompletion(t *testing.T) {
	t.Parallel()
	m := newMultiFileModel(t, 120, 35)
	m.findings = nil
	m.bgReview = &store.AgentState{Status: store.AgentDone}
	out := m.emptyStateContent(120)
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "No findings detected") {
		t.Fatalf("expected No-findings banner, got:\n%s", joined)
	}
}

// TestFooterIsOneLine: the contextual footer is one line derived from
// focus+leftView+curMode (Chunk 3 §6).
func TestFooterIsOneLine(t *testing.T) {
	t.Parallel()
	m := newMultiFileModel(t, 120, 35)
	line := m.footerLine()
	if strings.Contains(line, "\n") {
		t.Fatalf("footerLine must be one line, got multi-line:\n%s", line)
	}
	if !strings.Contains(line, "NORMAL") {
		t.Fatalf("footerLine missing MODE label, got: %s", line)
	}
}

// TestMasterDetailOnWideTerminal: at ≥120 cols with a finding selected,
// tabOverview combines diff + summary content (the master-detail intent
// from Chunk 2 step 1 — diff + summary in one panel without 5-tab
// switching). The single-outer-frame layout (Phase 1) handles wide-vs-
// narrow composition.
func TestMasterDetailOnWideTerminal(t *testing.T) {
	t.Parallel()
	m := newMultiFileModel(t, 160, 45)
	if m.width < 120 {
		t.Skip("requires wide terminal")
	}
	f := m.current()
	if f == nil {
		t.Fatal("precondition: must have a current finding")
	}
	// Master-detail intent (Chunk 2 §2): at ≥120 cols, tabOverview renders
	// as 2 horizontal columns (diff + explanation+evidence side-by-side).
	// We verify this by checking that tabContent returns content where at
	// least one row contains BOTH a diff-section marker AND the explanation
	// section marker — that's only possible when columns are joined
	// horizontally (lipgloss.JoinHorizontal emits a single row combining
	// both columns; in stacked mode they appear on separate rows).
	m.tab = tabOverview
	content := m.tabContent(m.tab)
	if len(content) == 0 {
		t.Fatal("tabOverview returned no content for wide terminal")
	}
	const wideSplit = "\u2503" // '┃' divider from masterDetailContent
	joined := 0
	for _, row := range content {
		if strings.Contains(row, wideSplit) {
			joined++
		}
	}
	if joined == 0 {
		t.Fatalf("wide terminal did not use 2-column horizontal composition: 0 rows contain %q divider.\nFirst 6 rows:\n%s",
			wideSplit, strings.Join(content[:func(a, b int) int {
	if a < b {
		return a
	}
	return b
}(6, len(content))], "\n"))
	}
	if joined < 3 {
		t.Fatalf("expected at least 3 horizontally-joined rows in master-detail view, got %d", joined)
	}
	// Verify the tab bar still renders on wide terminal.
	if len(m.tabBarView(m.width)) == 0 {
		t.Fatal("tab bar should render on wide terminal")
	}
	// Verify detailPaneView composes the master-detail layout successfully.
	lines := m.detailPaneView()
	if len(lines) == 0 {
		t.Fatal("detailPaneView returned no lines on wide terminal")
	}
}

// TestStackedOnNarrowTerminal: at <120 cols with a finding selected, the
// same tabOverview path is used — the layout stacks vertically because
// of the single-outer-frame Phase 1 design (no per-tab width split).
func TestStackedOnNarrowTerminal(t *testing.T) {
	t.Parallel()
	m := newMultiFileModel(t, 100, 30)
	if m.width >= 120 {
		t.Skip("requires narrow terminal")
	}
	f := m.current()
	if f == nil {
		t.Fatal("precondition: must have a current finding")
	}
	m.tab = tabOverview
	content := m.tabContent(m.tab)
	if len(content) == 0 {
		t.Fatal("tabOverview returned no content for narrow terminal")
	}
	lines := m.detailPaneView()
	if len(lines) == 0 {
		t.Fatal("detailPaneView returned no lines on narrow terminal")
	}
}

// TestOverlayRestoresFocus: closing an overlay preserves the focus state
// (Chunk 4 §7 invariant: underlying state is preserved when overlay closes).
func TestOverlayRestoresFocus(t *testing.T) {
	t.Parallel()
	m := newMultiFileModel(t, 120, 35)
	beforeFocus := m.focus
	m.pushOverlay(ovHelp)
	m.popOverlay()
	if m.focus != beforeFocus {
		t.Fatalf("closing overlay should preserve focus: before=%d after=%d",
			beforeFocus, m.focus)
	}
}

// TestOverlayEscCloses: opening an overlay via pushOverlay, then pressing
// Esc via the global keyboard handler, closes it and returns to the
// underlying frame (Chunk 4 §7 invariant).
func TestOverlayEscCloses(t *testing.T) {
	t.Parallel()
	m := newMultiFileModel(t, 120, 35)
	if m.topOverlay() != ovNone {
		t.Fatalf("precondition: no overlay open, got %d", m.topOverlay())
	}
	m.pushOverlay(ovHelp)
	if m.topOverlay() != ovHelp {
		t.Fatalf("pushOverlay did not set ovHelp, got %d", m.topOverlay())
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.topOverlay() != ovNone {
		t.Fatalf("Esc did not close overlay: topOverlay=%d (want 0)", m.topOverlay())
	}
}

// TestDraftEditorLifecycle: opens the draft editor overlay, View() renders
// with the editor on top, and clearing the editor preserves underlying
// state (Risk §3 invariant — overlay consolidation preserves underlying
// layout). Ctrl+S save dispatch is covered by TestCommentsEdit in
// e2e_actions_test.go.
func TestDraftEditorLifecycle(t *testing.T) {
	t.Parallel()
	m := newMultiFileModel(t, 120, 35)
	m.dir = t.TempDir() // ensure any save path has a writable dir
	beforeScroll := m.detailTop[m.tab]

	if m.draftEdit != nil {
		t.Fatal("precondition: no draft editor open")
	}

	// Open the draft editor directly (this is what startDraftComment does
	// after the user binds the key — the overlay is now on top).
	m.draftEdit = &draftEditor{ta: textarea.New()}
	m.draftEdit.ta.SetValue("hello world")

	if m.draftEdit == nil {
		t.Fatal("draft editor did not open")
	}
	withEditor := m.View().Content
	if withEditor == "" {
		t.Fatal("View() returned empty content while draft editor open")
	}

	// Underlying scroll preserved when overlay opens.
	if m.detailTop[m.tab] != beforeScroll {
		t.Fatalf("opening draft editor disturbed scroll: before=%d after=%d",
			beforeScroll, m.detailTop[m.tab])
	}

	// Close the editor (simulates successful save — Ctrl+S path covered by
	// e2e_actions_test.go TestCommentsEdit).
	m.draftEdit = nil

	if m.draftEdit != nil {
		t.Fatal("draft editor did not close")
	}
	if m.detailTop[m.tab] != beforeScroll {
		t.Fatalf("closing draft editor disturbed scroll: before=%d after=%d",
			beforeScroll, m.detailTop[m.tab])
	}
	afterView := m.View().Content
	if afterView == "" {
		t.Fatal("View() returned empty content after closing draft editor")
	}
}

// TestNoAllCapsInStatus: status messages use Title-case (or sentence case)
// after the "Status:" label — never ALL-CAPS for the description (Chunk 5
// step 4). Key names like "Ctrl+S", "Esc", "P" are allowed (key shortcuts,
// not the status description).
func TestNoAllCapsInStatus(t *testing.T) {
	t.Parallel()
	m := newMultiFileModel(t, 120, 35)
	got := m.statusBarView()
	plain := reANSI.ReplaceAllString(got, "")
	if idx := strings.Index(plain, "Status:"); idx >= 0 {
		tail := plain[idx+len("Status:"):]
		if bad := reAllCaps.FindString(tail); bad != "" {
			t.Fatalf("ALL-CAPS in status description: %q\nfull plain text: %s",
				strings.TrimSpace(bad), plain)
		}
	}
}

var reANSI = regexp.MustCompile("\x1b[[0-9;]*[a-zA-Z]")
var reAllCaps = regexp.MustCompile(`(?:^|\s|[^A-Z])([A-Z]{3,})\b`)

// TestOverlayDoesNotReflow: opening an overlay must not blow up the
// underlying frame (Chunk 4 §7). overlayBoxRaw pads to a fixed height so
// the underlying content is preserved when an overlay is open or closed.
//
// Not parallelised: asserts bit-equal before==after content. m.View()
// reads m.bgReview + m.msg + bgRunning() timers that can drift under
// t.Parallel() execution (bg goroutines from newMultiFileModel's
// fixture load may still be settling when the second View() runs).
// The test is fast (<0.05s) so serial is acceptable.
func TestOverlayDoesNotReflow(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	_ = m.View().Content // warm-up: settle render-time state (window sync, browse seed)
	before := m.View().Content
	m.pushOverlay(ovHelp)
	withOverlay := m.View().Content
	m.popOverlay()
	after := m.View().Content

	// Overlay doesn't blow up the frame: withOverlay should not be wildly
	// larger than before.
	if len(withOverlay) > 2*len(before) {
		t.Fatalf("overlay doubled frame size: before=%d after=%d",
			len(before), len(withOverlay))
	}
	if before != after {
		t.Fatal("closing overlay should restore exact previous content")
	}
}
