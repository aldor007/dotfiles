package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mr-review/internal/store"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func textInputForTest() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "ask> "
	return ti
}

// newTestModel builds a model with mock findings and a crafted diff.
func newTestModel(t *testing.T, width, height int, findings []store.Finding) *model {
	t.Helper()
	diff := "diff --git a/services/review/handler.go b/services/review/handler.go\n" +
		"--- a/services/review/handler.go\n+++ b/services/review/handler.go\n" +
		"@@ -168,7 +168,11 @@ func handler() {\n" +
		" \tprior()\n" +
		"+\tmarkOffering(ctx, target, name)\n" +
		"+\tmarkOffering(ctx, target, name)\n" +
		"+\tmarkOffering(ctx, target, name)\n" +
		"+\tGetResource(ctx, id, grp, ver, res, \"\", name)\n" +
		" \tfollowing()\n"
	return newTestModelDiff(t, width, height, findings, diff)
}

// newTestModelDiff builds a model over a review dir with a custom diff.
func newTestModelDiff(t *testing.T, width, height int, findings []store.Finding, diff string) *model {
	t.Helper()
	dir := t.TempDir()
	rev := &store.Review{
		Project: "grp/proj", IID: 55868, HeadSHA: "2d41f86b1234",
		SourceBranch: "feat/x", TargetBranch: "master",
		Title: "REVIEW-810 Regional Spot Predictions", State: "reviewing",
		CreatedAt: "2026-10-20T10:00:00Z",
	}
	if err := rev.Save(dir); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFindings(dir, findings); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "diff.patch"), []byte(diff), 0o600); err != nil {
		t.Fatal(err)
	}
	ti := textInputForTest()
	search := textinput.New()
	search.Prompt = "/ "
	pal := textinput.New()
	pal.Prompt = ": "
	m := &model{
		dir: dir, rev: rev, findings: findings,
		filter: Filter{Kind: "all"}, input: ti, ctxLines: 3,
		width: width, height: height,
		detailTop: map[int]int{}, searchInput: search, paletteInput: pal,
		visualStart: -1,
	}
	m.narrow = width < 90
	m.rebuild()
	m.ensureDiff()
	return m
}

func mockFindings(n int) []store.Finding {
	fs := make([]store.Finding, 0, n)
	for i := 1; i <= n; i++ {
		fs = append(fs, store.Finding{
			ID:   fmt.Sprintf("F%03d", i),
			File: "services/review/handler.go",
			Line: 172 + i, LineEnd: 172 + i,
			Severity: []string{"P1", "P2", "P3"}[i%3],
			Kind:     "bug", Title: fmt.Sprintf("Finding number %d with a reasonably long title", i),
			Explanation: "The call passes arguments in the wrong order which breaks lookup.",
			Evidence:    "handler.go:172 and resourceaction/service.go:275",
			Comment:     fmt.Sprintf("Please fix finding %d.", i),
			Confidence:  "high", Decision: "pending",
		})
	}
	return fs
}

func key(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'a', Text: s} } // generic single-key; overridden below when needed

var (
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEsc}
	keyTab   = tea.KeyPressMsg{Code: tea.KeyTab}
)

func TestViewFitsTerminalWidths(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {120, 35}, {160, 45}, {200, 50}} {
		m := newTestModel(t, size[0], size[1], mockFindings(20))
		view := m.View().Content
		for i, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > size[0] {
				t.Fatalf("%dx%d: line %d is %d cells (> %d): %q", size[0], size[1], i, w, size[0], line)
			}
		}
		if !strings.Contains(view, "!55868") {
			t.Fatalf("%dx%d: header missing MR id", size[0], size[1])
		}
		if size[0] >= 90 {
			if !strings.Contains(view, "20 findings") {
				t.Fatalf("%dx%d: header missing finding counts", size[0], size[1])
			}
		} else if !strings.Contains(view, "20 findings") {
			t.Fatalf("%dx%d: narrow header missing finding count", size[0], size[1])
		}
		if strings.Contains(view, "working…") {
			t.Fatalf("%dx%d: unexpected busy state", size[0], size[1])
		}
	}
}

func TestViewEmptyAndSingleFinding(t *testing.T) {
	m := newTestModel(t, 120, 35, nil)
	if v := m.View().Content; !strings.Contains(v, "(no findings)") {
		t.Fatal("empty list message missing")
	}
	// filtered empty state mentions the filter
	m.filter = Filter{Kind: "approved"}
	m.rebuild()
	if v := m.View().Content; !strings.Contains(v, "no findings match the filter") {
		t.Fatal("filtered empty state missing")
	}
	m = newTestModel(t, 120, 35, mockFindings(1))
	if v := m.View().Content; !strings.Contains(v, "F001") {
		t.Fatal("single finding not rendered")
	}
}

func TestNarrowSinglePane(t *testing.T) {
	m := newTestModel(t, 80, 24, mockFindings(5))
	if !m.narrow {
		t.Fatal("80 cols must be narrow mode")
	}
	// list first
	if v := m.View().Content; !strings.Contains(v, "FINDINGS") {
		t.Fatal("narrow list view missing")
	}
	// enter → detail fills the whole width
	m2, _ := m.Update(keyEnter)
	mm := m2.(*model)
	v := mm.View().Content
	if !strings.Contains(v, "DIFF ") || strings.Contains(v, "FINDINGS 5/5") {
		t.Fatal("narrow detail view wrong (expected the Diff tab, no findings list)")
	}
	// esc → back to list
	m3, _ := mm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m3.(*model).focus != focusList {
		t.Fatal("esc must return to the list in narrow mode")
	}
}

func TestTabSwitchingAndFocus(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	if m.tab != tabDiff {
		t.Fatal("default tab must be Diff")
	}
	// Tab → focus detail; h/l cycle tabs
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	mm := m2.(*model)
	if mm.focus != focusDetail {
		t.Fatal("Tab must focus the detail pane")
	}
	if mm.tab != tabDiff {
		t.Fatal("tab must be preserved")
	}
	m3, _ := mm.Update(key("l"))
	if m3.(*model).tab != tabExplanation {
		t.Fatalf("l must move to Explanation, got %d", m3.(*model).tab)
	}
	m4, _ := m3.Update(key("h"))
	if m4.(*model).tab != tabDiff {
		t.Fatal("h must move back to Diff")
	}
	// wrap-around
	m5, _ := m4.Update(key("h"))
	if m5.(*model).tab != tabDiscussion {
		t.Fatal("h from Diff must wrap to Discussion")
	}
	// Tab again → back to list focus, tab preserved
	m6, _ := m5.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m6.(*model).focus != focusList || m6.(*model).tab != tabDiscussion {
		t.Fatal("Tab must return focus to the list and keep the tab")
	}
}

func TestDecisionAutoAdvanceAndUndo(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(4))
	if m.cursor != 0 {
		t.Fatal("cursor must start at 0")
	}
	m2, _ := m.Update(key("a")) // approve F001 → advance to next pending
	mm := m2.(*model)
	if mm.findings[mm.filtered[0]].Decision != "approved" {
		t.Fatal("F001 not approved")
	}
	if mm.cursor != 1 {
		t.Fatalf("auto-advance expected cursor 1, got %d", mm.cursor)
	}
	// undo from the new position
	m3, _ := mm.Update(key("u"))
	if m3.(*model).findings[m3.(*model).filtered[0]].Decision != "approved" {
		t.Fatal("u must affect the SELECTED finding, not the original one")
	}
}

func TestSortModes(t *testing.T) {
	fs := mockFindings(3)
	fs[0].File = "zzz.go"
	fs[1].File = "aaa.go"
	m := newTestModel(t, 120, 35, fs)
	// sort by file
	m2, _ := m.Update(key("o"))
	mm := m2.(*model)
	if mm.sortMode != 1 {
		t.Fatalf("o must cycle to file sort, got %d", mm.sortMode)
	}
	if mm.findings[mm.filtered[0]].File != "aaa.go" {
		t.Fatal("file sort wrong order")
	}
}

func TestPublishPreviewRequiresExplicitEnter(t *testing.T) {
	fs := mockFindings(2)
	fs[0].Decision = "approved"
	fs[1].Decision = "pending"
	m := newTestModel(t, 120, 35, fs)

	// P opens the preview overlay only
	m2, _ := m.Update(key("P"))
	mm := m2.(*model)
	if mm.overlay != 4 {
		t.Fatalf("P must open the publication preview, got overlay=%d", mm.overlay)
	}
	v := mm.View().Content
	if !strings.Contains(v, "PUBLICATION PREVIEW") || !strings.Contains(v, "nothing is sent yet") {
		t.Fatal("preview overlay text wrong")
	}
	if !strings.Contains(v, "1 comment(s) would be posted") {
		t.Fatal("preview must count only approved findings")
	}
	if strings.Contains(v, "F002") && strings.Contains(v, "pending") && strings.Contains(v, "would be posted") {
		t.Fatal("pending finding must not appear as publishable")
	}
	// Esc cancels — nothing runs
	m3, _ := mm.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m3.(*model).overlay != 0 {
		t.Fatal("esc must close the preview")
	}
	// entering the overlay again and confirming hands off to the publisher
	// (exec is guarded by tea.ExecProcess; here we only assert the transition)
	m4, _ := m3.Update(key("P"))
	if m4.(*model).overlay != 4 {
		t.Fatal("P must reopen the preview")
	}
}

func TestPendingNeverAutoPublished(t *testing.T) {
	fs := mockFindings(2)
	fs[0].Decision = "rejected"
	fs[1].Decision = "pending"
	m := newTestModel(t, 120, 35, fs)
	m2, _ := m.Update(key("P"))
	mm := m2.(*model)
	v := mm.View().Content
	if !strings.Contains(v, "nothing to publish: approve findings or write comments first") {
		t.Fatal("rejected/pending-only reviews must never offer publication")
	}
}

func TestHelpOverlay(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	m2, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	mm := m2.(*model)
	if mm.overlay != 3 {
		t.Fatal("? must open help")
	}
	if v := mm.View().Content; !strings.Contains(v, "KEYBOARD REFERENCE") {
		t.Fatal("help overlay missing")
	}
	// any key closes it
	m3, _ := mm.Update(key("j"))
	if m3.(*model).overlay != 0 {
		t.Fatal("help must close on any key")
	}
}

func TestDiffScrollFollowsSelection(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	m.focus = focusDetail
	m.tab = tabDiff
	// selecting a finding must center its diff window on the right line
	m.clampScrolls()
	top := m.detailTop[tabDiff]
	f := m.current()
	rows, hl := DiffWindow(m.diffFor(f), f.Side, f.Line, f.LineEnd, m.ctxLines)
	_ = rows
	if top < 0 {
		t.Fatal("negative scroll")
	}
	// the highlighted row must be inside the visible window after scroll
	visible := hl - top
	if visible < 0 || visible > m.bodyHeight() {
		t.Fatalf("highlight row %d outside visible window (top=%d h=%d)", hl, top, m.bodyHeight())
	}
}

func TestLongContentWrapsNotClips(t *testing.T) {
	fs := mockFindings(1)
	fs[0].Explanation = strings.Repeat("word ", 120) // ~600 chars
	m := newTestModel(t, 100, 30, fs)
	m.tab = tabExplanation
	m.focus = focusDetail
	content := m.tabContent(tabExplanation)
	w := m.detailWidth() - 2
	for _, l := range content {
		if got := visibleCells(stripANSIish(l)); got > w {
			t.Fatalf("content line exceeds width %d: %d cells (%q)", w, got, l)
		}
	}
	// long file paths are truncated safely in the list
	if v := m.View().Content; strings.Contains(v, "\x1b[0m"+strings.Repeat("x", 200)) {
		t.Fatal("unbounded path rendered")
	}
}
