package tui

import (
	"fmt"
	"strings"
	"testing"

	"mr-review/internal/store"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// press sends a key to the model and returns the updated model.

func pressKey(m *model, k string) *model {
	m2, _ := m.Update(key(k))
	return m2.(*model)
}

// stateCheck asserts the core consistency invariant: selected finding ID,
// highlighted list row (when the list is visible), details content, and
// counters all agree.
func stateCheck(t *testing.T, m *model, wantID string) {
	t.Helper()
	cur := m.current()
	if wantID == "" {
		if cur != nil {
			t.Fatalf("expected no selection, got %s", cur.ID)
		}
		return
	}
	if cur == nil || cur.ID != wantID {
		got := "<nil>"
		if cur != nil {
			got = cur.ID
		}
		t.Fatalf("selected finding = %s, want %s", got, wantID)
	}
	v := m.View().Content
	plain := stripANSIish(v)
	// the details region ("SELECTED FINDING") renders in the findings pane on
	// wide terminals and in the floating drawer on medium ones; narrow mode
	// shows the list alone, so only assert details when they are on screen
	L := m.layout()
	detailsVisible := L.mode == layoutWide || (L.mode == layoutMedium && m.focus == focusFindings)
	listVisible := L.mode == layoutWide || m.focus == focusFindings
	if detailsVisible && !strings.Contains(plain, "SELECTED FINDING") {
		t.Fatal("findings details region missing")
	}
	// the selected ID must appear in a highlighted list row — only when the
	// findings list is actually rendered
	if listVisible {
		hl := false
		for _, l := range strings.Split(plain, "\n") {
			if (strings.Contains(l, "▌") || strings.Contains(l, "> ")) && strings.Contains(l, wantID) {
				hl = true
				break
			}
		}
		if !hl {
			t.Fatalf("list row for %s not highlighted", wantID)
		}
	}
}

func counts(t *testing.T, m *model) (ap, pe, rj, pub int) {
	t.Helper()
	return m.decisionCounts()
}

// 1+2: reject/approve + auto-advance — message names the actually selected ID.
func TestRejectApproveAutoAdvance(t *testing.T) {
	for _, act := range []string{"r", "a"} {
		m := newTestModel(t, 120, 35, mockFindings(4))
		acted := m.currentID()
		wantNext := "F001" // first pending after the acted finding, by severity sort
		for _, idx := range m.filtered[1:] {
			if f := &m.findings[idx]; f.Decision == "pending" && f.Publication == nil {
				wantNext = f.ID
				break
			}
		}
		m2 := pressKey(m, act)
		if m2.currentID() != wantNext {
			t.Fatalf("%s: expected advance to %s, selected %s", act, wantNext, m2.currentID())
		}
		wantVerb := "rejected"
		if act == "a" {
			wantVerb = "approved"
		}
		if !strings.Contains(m2.msg, fmt.Sprintf("%s %s → next pending %s", acted, wantVerb, wantNext)) {
			t.Fatalf("%s: wrong message %q", act, m2.msg)
		}
		stateCheck(t, m2, wantNext)
		ap, pe, rj, _ := counts(t, m2)
		if act == "r" && (ap != 0 || pe != 3 || rj != 1) {
			t.Fatalf("counts after reject: ap=%d pe=%d rj=%d", ap, pe, rj)
		}
		if act == "a" && (ap != 1 || pe != 3 || rj != 0) {
			t.Fatalf("counts after approve: ap=%d pe=%d rj=%d", ap, pe, rj)
		}
	}
}

// 3: no pending findings remain → completion message, no advance.
func TestNoPendingRemaining(t *testing.T) {
	fs := mockFindings(2)
	fs[0].Decision = "approved"
	fs[1].Decision = "approved"
	m := newTestModel(t, 120, 35, fs)
	acted := m.currentID()
	m2 := pressKey(m, "r") // decide the last pending → complete
	if !strings.Contains(m2.msg, "review complete") {
		t.Fatalf("expected completion message, got %q", m2.msg)
	}
	if m2.currentID() != acted {
		t.Fatalf("selection must stay on the acted finding, got %s", m2.currentID())
	}
	stateCheck(t, m2, acted)
	// status bar shows the persistent completion state
	if v := m2.View().Content; !strings.Contains(v, "review complete") {
		t.Fatal("status bar must show review-complete state")
	}
}

// 4: undo after auto-advance affects the SELECTED finding and restores pending.
func TestUndoAfterAdvance(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	acted := m.currentID()
	m2 := pressKey(m, "a") // approve the first finding, cursor → next pending
	next := m2.currentID()
	if next == acted {
		t.Fatal("advance failed")
	}
	m3 := pressKey(m2, "u") // undo on the SELECTED finding
	for i := range m3.findings {
		if m3.findings[i].ID == acted && m3.findings[i].Decision != "approved" {
			t.Fatal("u must not touch the previously acted finding")
		}
		if m3.findings[i].ID == next && m3.findings[i].Decision != "pending" {
			t.Fatal("u must revert the selected finding to pending")
		}
	}
	if !strings.Contains(m3.msg, next) {
		t.Fatalf("undo message must name %s, got %q", next, m3.msg)
	}
	stateCheck(t, m3, next)
}

// 5: published findings are skipped by auto-advance and never re-offered.
func TestPublishedSkipped(t *testing.T) {
	fs := mockFindings(3)
	fs[0].Publication = &store.Publication{DiscussionID: "D1"}
	m := newTestModel(t, 120, 35, fs)
	// start with published F001 selected (cursor 0), reject → must skip to F002
	m2 := pressKey(m, "r")
	if m2.currentID() != "F002" {
		t.Fatalf("published F001 must be skipped by advance, selected %s", m2.currentID())
	}
	// F002 now rejected; only F003 pending
	m3 := pressKey(m2, "u")
	_ = m3
	// publish preview must never list published findings
	m4 := newTestModel(t, 120, 35, fs)
	m4.findings[1].Decision = "approved"
	v := strings.Join(m4.publishPreviewOverlay(120, 30), "\n")
	if strings.Contains(stripANSIish(v), "F001") {
		t.Fatal("published finding offered for publication")
	}
}

// 6: filtering keeps the selection when visible; otherwise nearest visible.
func TestFilterKeepsSelection(t *testing.T) {
	fs := mockFindings(4)
	fs[0].Decision = "approved"
	m := newTestModel(t, 120, 35, fs)
	m.cursor = 0           // first in severity sort (F003 here, still pending)
	m.applyFilterChoice(2) // pending filter
	if m.filter.Kind != "pending" {
		t.Fatal("filter choice 2 must be pending")
	}
	if m.currentID() != "F003" {
		t.Fatalf("visible pending selection must be preserved, got %s", m.currentID())
	}
	stateCheck(t, m, "F003")
	// selection that leaves the filter falls to the nearest visible pending
	m2 := newTestModel(t, 120, 35, fs)
	m2.cursor = 0
	m2.findings[2].Decision = "approved" // make the selected one non-pending
	m2.rebuild()
	m2.applyFilterChoice(2)
	if m2.currentID() == "" || m2.currentID() == "F003" {
		t.Fatalf("hidden selection must fall to a visible pending, got %q", m2.currentID())
	}
	stateCheck(t, m2, m2.currentID())
}

// 7: switching tabs never resets review decisions.
func TestTabSwitchPreservesDecisions(t *testing.T) {
	fs := mockFindings(3)
	fs[0].Decision = "rejected"
	m := newTestModel(t, 120, 35, fs)
	acted := m.currentID()
	m2 := pressKey(m, "a")                                      // approve the first finding
	r, _ := m2.Update(tea.WindowSizeMsg{Width: 79, Height: 24}) // h/l are narrow-mode tab bindings
	mn := r.(*model)
	mm := pressKey(mn, "tab") // tab cycles focus (diff → findings): decisions persist
	mm = pressKey(mm, "2")    // back to the diff pane: h/l cycle detail tabs there
	mm = pressKey(mm, "l")
	mm = pressKey(mm, "l")
	mm = pressKey(mm, "l")
	if mm.tab != tabComment {
		t.Fatalf("h/l must reach the Comment tab, got %d", mm.tab)
	}
	for i := range mm.findings {
		if mm.findings[i].ID == acted && mm.findings[i].Decision != "approved" {
			t.Fatal("tab switching reset the decision")
		}
	}
	// approving auto-advances; the Comment tab honestly shows the CURRENT
	// finding's decision. Jump the findings cursor back to the approved
	// finding and assert its decision is still rendered.
	for i, idx := range mm.filtered {
		if mm.findings[idx].ID == acted {
			mm.cursor = i
			break
		}
	}
	mm = pressKey(mm, "2")
	v := mm.View().Content
	if !strings.Contains(stripANSIish(v), "approved") {
		t.Fatal("comment tab must show the decision")
	}
}

// 8: overview tab scroll position is preserved when switching to comment tab.
func TestDetailsScrollIndependent(t *testing.T) {
	fs := mockFindings(1)
	fs[0].Explanation = strings.Repeat("word ", 400)
	fs[0].Evidence = strings.Repeat("ref ", 400)
	m := newTestModel(t, 120, 35, fs)
	m.focus = focusDiff
	m.tab = tabOverview
	m = pressKey(m, "j") // scroll overview down
	topOverview := m.detailTop[tabOverview]
	if topOverview <= 0 {
		t.Fatal("overview must scroll")
	}
	// switch to comment tab and back
	m.tab = tabComment
	if m.detailTop[tabComment] != 0 {
		t.Fatal("comment tab must start at top")
	}
	m.tab = tabOverview
	if m.detailTop[tabOverview] != topOverview {
		t.Fatal("overview scroll position must be preserved across tab switches")
	}
}

// 9: resizing keeps focus, selection and decisions; layout still fits.
func TestResizeKeepsState(t *testing.T) {
	m := newTestModel(t, 160, 45, mockFindings(5))
	acted := m.currentID()
	m2 := pressKey(m, "r")
	sel := m2.currentID()
	mm := pressKey(m2, "tab")
	mm2, _ := mm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m3 := mm2.(*model)
	if m3.layout().mode != layoutMedium {
		t.Fatal("80 cols must switch to medium mode (sidebar+diff)")
	}
	if m3.currentID() != sel || m3.focus != focusDiff {
		t.Fatalf("resize lost state: sel=%s focus=%d", m3.currentID(), m3.focus)
	}
	for i := range m3.findings {
		if m3.findings[i].ID == acted && m3.findings[i].Decision != "rejected" {
			t.Fatal("resize lost decisions")
		}
	}
	stateCheck(t, m3, sel)
	mm3, _ := m3.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	m5 := mm3.(*model)
	if !m5.narrow {
		t.Fatal("70 cols must switch to narrow mode")
	}
	if m5.currentID() != sel {
		t.Fatalf("resize lost selection: sel=%s", m5.currentID())
	}
	mm4, _ := m5.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m4 := mm4.(*model)
	if m4.narrow {
		t.Fatal("200 cols must be wide mode")
	}
	stateCheck(t, m4, sel)
}

// 10: reopening a persisted review restores decisions and selection.
func TestReopenPersistedReview(t *testing.T) {
	dir := t.TempDir()
	rev := &store.Review{Project: "p", IID: 1, HeadSHA: "abcdef123456",
		SourceBranch: "a", TargetBranch: "b", Title: "T", State: "open"}
	if err := rev.Save(dir); err != nil {
		t.Fatal(err)
	}
	fs := mockFindings(3)
	fs[0].Decision = "approved"
	fs[1].Decision = "rejected"
	if err := store.SaveFindings(dir, fs); err != nil {
		t.Fatal(err)
	}
	// simulate Run()'s load path
	loaded, err := store.LoadFindings(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t, 120, 35, loaded)
	if m.findings[0].Decision != "approved" || m.findings[1].Decision != "rejected" {
		t.Fatal("persisted decisions lost on reopen")
	}
	// reload() (fileChangedMsg path) must also preserve them
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	if m.findings[0].Decision != "approved" {
		t.Fatal("reload lost decisions")
	}
}

// 11: publication reflected after refresh — counts and preview agree.
func TestRefreshAfterPublication(t *testing.T) {
	fs := mockFindings(3)
	fs[0].Decision = "approved"
	m := newTestModel(t, 120, 35, fs)
	// publisher ran outside: F001 got published on disk
	fs2 := mockFindings(3)
	fs2[0].Publication = &store.Publication{DiscussionID: "D42"}
	if err := store.SaveFindings(m.dir, fs2); err != nil {
		t.Fatal(err)
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	_, _, _, pub := counts(t, m)
	if pub != 1 {
		t.Fatalf("published count = %d, want 1", pub)
	}
	if m.findings[0].Publication == nil || m.findings[0].Publication.DiscussionID != "D42" {
		t.Fatal("publication metadata lost")
	}
	v := stripANSIish(m.View().Content)
	if !strings.Contains(v, "published") {
		t.Fatal("published state not rendered")
	}
}

// 12: focus switching keeps selection and counts consistent.
func TestFocusSwitchConsistency(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(4))
	m2 := pressKey(m, "r") // reject the first finding → auto-advance
	sel := m2.currentID()
	mm := pressKey(m2, "tab")
	if mm.focus != focusDiff {
		t.Fatal("Tab must focus details")
	}
	stateCheck(t, mm, sel)
	mm = pressKey(mm, "j") // scrolls details, selection unchanged
	if mm.currentID() != sel {
		t.Fatal("detail scrolling must not change the selection")
	}
	mm = pressKey(mm, "tab")
	if mm.focus != focusFindings {
		t.Fatal("Tab must return to the list")
	}
	stateCheck(t, mm, sel)
}

// Geometry: the FINDINGS pane header renders across the full pane width and
// body rows are perfectly framed (one leading │, one trailing │, no ragged
// right edge). (Repurposed from TestTabBarSpansPaneWidth: the wide right
// pane no longer has a segmented tab bar.)
func TestFindingsPaneHeaderSpansPaneWidth(t *testing.T) {
	for _, sz := range [][2]int{{120, 35}, {160, 45}, {200, 50}} {
		m := newTestModel(t, sz[0], sz[1], mockFindings(3))
		m.focus = focusFindings
		L := m.layout()
		wantW := L.findingsW + 2
		// findings pane column range inside the composed side-by-side body:
		// files box (fw+2) + gap + diff box (dw+2) + gap
		fx := L.filesW + L.diffW + 6
		found := false
		for _, r := range strings.Split(m.View().Content, "\n") {
			if !strings.Contains(stripANSIish(r), "FINDINGS") {
				continue
			}
			found = true
			pane := keepCells(skipCells(r, fx), wantW) // column slice, ANSI-safe
			if lipgloss.Width(pane) != wantW {
				t.Fatalf("%dx%d: findings pane row width %d != pane width %d", sz[0], sz[1], lipgloss.Width(pane), wantW)
			}
			plain := stripANSIish(pane)
			if !strings.HasPrefix(plain, "│") || !strings.HasSuffix(plain, "│") {
				t.Fatalf("%dx%d: findings pane header row not framed: %q", sz[0], sz[1], plain)
			}
			break
		}
		if !found {
			t.Fatalf("%dx%d: FINDINGS pane header not rendered inside the body", sz[0], sz[1])
		}
	}
}

// The footer must not duplicate the transient status message.
func TestFooterNoDuplicateMessage(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	m2 := pressKey(m, "r")
	v := m2.View().Content
	lines := strings.Split(v, "\n")
	last := stripANSIish(lines[len(lines)-1])
	if strings.Count(stripANSIish(v), "next pending") > 1 {
		t.Fatal("action message duplicated")
	}
	if strings.Contains(last, "next pending") {
		t.Fatal("footer must not repeat the status message")
	}
}

// Hunk navigation and horizontal scrolling in the Diff tab.
func TestDiffHunkNavAndHorizontalScroll(t *testing.T) {
	// two hunks
	diff := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n" +
		"@@ -1,3 +1,4 @@\n a\n+b\n c\n d\n" +
		"@@ -40,3 +41,4 @@\n e\n+f\n g\n h\n"
	fs := []store.Finding{{ID: "F001", File: "x.go", Line: 2, LineEnd: 2,
		Severity: "P1", Kind: "bug", Title: "t", Decision: "pending"}}
	m := newTestModelDiff(t, 120, 35, fs, diff)
	m.focus = focusDiff
	m.tab = tabOverview
	if m.diffFor(m.current()) == nil {
		t.Fatal("diff not parsed")
	}
	before := m.detailTop[tabOverview]
	m2 := pressKey(m, "]")
	if m2.detailTop[tabOverview] == before && m2.ctxLines == m.ctxLines {
		t.Fatal("] must move to the next hunk or expand context")
	}
	m3 := pressKey(m, ",")
	if m3.diffHOff != 0 {
		t.Fatal(", at offset 0 must stay at 0")
	}
	m4 := pressKey(m3, ".")
	if m4.diffHOff != 24 {
		t.Fatalf(". must scroll right by 24, got %d", m4.diffHOff)
	}
	// rendering with offset must not exceed width
	w := m4.detailWidth()
	for _, l := range m4.tabContent(tabOverview) {
		if got := lipgloss.Width(l); got > w {
			t.Fatalf("h-scrolled line %d cells > %d", got, w)
		}
	}
}
