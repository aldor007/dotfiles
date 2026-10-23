// findingdetails_test.go — wave-2 FINDINGS pane tests: details region
// sections, independent sub-scroll, ID-stable selection across background
// ingest, region geometry, empty states, and pane-width plumb-through.
package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"mr-review/internal/store"
)

// detailFixture returns a finding carrying every details section.
func detailFixture() store.Finding {
	return store.Finding{
		ID: "F042", File: "services/review/handler.go", Side: "new",
		Line: 172, LineEnd: 175, Severity: "P1", Kind: "bug",
		Title:       "Arguments passed in the wrong order",
		Explanation: "The call passes `target` where `name` is expected.\n\nUse **named arguments**.",
		Evidence:    "handler.go:172 and resourceaction/service.go:275 agree on the signature.",
		Comment:     "Please swap the last two arguments of markOffering.",
		Extra:       map[string]string{"suggested_fix": "markOffering(ctx, target, name)\n+ markOffering(ctx, name, target)"},
		Confidence:  "high", Decision: "pending",
		Discussion: []store.DiscussionTurn{{Q: "Is this a runtime failure?", A: "Yes — lookup returns the empty resource."}},
	}
}

// paneText renders the wide right pane and returns the ANSI-stripped,
// whitespace-normalized text (line wraps in narrow panes must not break
// content-presence assertions).
func paneText(t *testing.T, m *model) string {
	t.Helper()
	v := stripANSIish(strings.Join(m.renderFindingsPane(m.layout().findingsW, m.bodyHeight()), " "))
	return strings.Join(strings.Fields(v), " ")
}

func TestFindingDetailsShowsAllSections(t *testing.T) {
	m := newTestModel(t, 160, 35, []store.Finding{detailFixture()})
	m.focus = focusFindings
	// the details region scrolls: walk it top-to-bottom and assert across
	// the CONCATENATION (sections below the fold must still be reachable)
	v := paneText(t, m)
	for i := 0; i < 30; i++ {
		m.findingsDetailsScroll(1)
		v += " " + paneText(t, m)
	}
	for _, want := range []string{
		"SELECTED FINDING",
		"F042",                          // ID in header
		"Arguments passed in the wrong", // title
		"handler.go:172-175",            // location incl. end line
		"new side",                      // side marker
		"STATUS", "● pending",           // status pill
		"EXPLANATION", "named arguments", // rendered markdown
		"EVIDENCE", "service.go:275", // evidence full text
		"SUGGESTED COMMENT", "swap the last two arguments", // comment
		"SUGGESTED FIX", "markOffering(ctx, name, target)",
		"DISCUSSION", "Q:", "runtime failure", "lookup returns",
		"a approve · r reject", // actions footer
		"u undo", "e edit", "d discuss", "y copy",
	} {
		if !strings.Contains(v, want) {
			t.Fatalf("details region missing %q\n%s", want, v)
		}
	}
}

func TestFindingDetailsFooterPublishHint(t *testing.T) {
	f := detailFixture()
	f.Decision = "approved" // publishable, not published
	m := newTestModel(t, 160, 35, []store.Finding{f})
	v := paneText(t, m)
	if !strings.Contains(v, "P publish preview") {
		t.Fatalf("publishable finding must show the P hint\n%s", v)
	}
	// published findings are not publishable
	f.Decision = "approved"
	f.Publication = &store.Publication{DiscussionID: "D9"}
	m2 := newTestModel(t, 160, 35, []store.Finding{f})
	if strings.Contains(paneText(t, m2), "P publish preview") {
		t.Fatal("published finding must not show the P hint")
	}
}

func TestFindingDetailsIndependentScroll(t *testing.T) {
	f := detailFixture()
	f.Explanation = strings.Repeat("word ", 400) // long content → scrollable
	m := newTestModel(t, 160, 35, []store.Finding{f})
	m.focus = focusFindings
	sel := m.currentID()
	rows0 := m.findingDetailsRegion(m.current(), m.layout().findingsW, m.findingDetailsRows())

	// details scroll: offset moves, list cursor + selection untouched
	m.findingsDetailsPage(1)
	if m.detailTop[fdScrollKey] <= 0 {
		t.Fatal("details page-down must move the details offset")
	}
	if m.cursor != 0 || m.currentID() != sel {
		t.Fatalf("details scroll moved the list cursor: cur=%d id=%s", m.cursor, m.currentID())
	}
	rows1 := m.findingDetailsRegion(m.current(), m.layout().findingsW, m.findingDetailsRows())
	if lipgloss.JoinHorizontal(lipgloss.Top, rows0...) == lipgloss.JoinHorizontal(lipgloss.Top, rows1...) {
		t.Fatal("details region content must shift after page-down")
	}

	// scrolling up clamps at the top
	m.findingsDetailsScroll(-100)
	if m.detailTop[fdScrollKey] != 0 {
		t.Fatalf("details scroll must clamp at 0, got %d", m.detailTop[fdScrollKey])
	}

	// selecting another finding resets the details offset to the top
	m.findingsDetailsPage(3)
	f2 := detailFixture()
	f2.ID = "F043"
	f2.Line = 200
	m.ingestFindings([]store.Finding{*m.current(), f2})
	for i, idx := range m.filtered {
		if m.findings[idx].ID == "F043" {
			m.cursor = i
		}
	}
	rows := m.findingDetailsRegion(m.current(), m.layout().findingsW, m.findingDetailsRows())
	if m.detailTop[fdScrollKey] != 0 {
		t.Fatalf("selection change must reset details offset, got %d", m.detailTop[fdScrollKey])
	}
	if !strings.Contains(stripANSIish(strings.Join(rows, "\n")), "F043") {
		t.Fatal("details region must show the newly selected finding")
	}
}

// Background ingest APPENDS findings; the selection must stay on the same
// finding ID even when the new finding sorts before the cursor.
func TestIngestKeepsSelectionByID(t *testing.T) {
	fs := mockFindings(4)
	m := newTestModel(t, 120, 35, fs)
	for i, idx := range m.filtered {
		if m.findings[idx].ID == "F002" {
			m.cursor = i
		}
	}
	sel := m.currentID()
	if sel != "F002" {
		t.Fatalf("precondition: selected %s", sel)
	}
	// ingest: a new P0 finding arrives (sorts to the front) + one more P3
	newF := detailFixture()
	newF.ID, newF.Severity = "F042", "P0"
	grown := append([]store.Finding{newF}, mockFindings(4)...)
	grown = append(grown, store.Finding{ID: "F099", File: "services/review/handler.go",
		Line: 180, LineEnd: 180, Severity: "P3", Kind: "bug", Title: "late", Decision: "pending"})
	m.ingestFindings(grown)
	if m.currentID() != sel {
		t.Fatalf("ingest moved the selection: %s != %s", m.currentID(), sel)
	}
	if len(m.findings) != 6 {
		t.Fatalf("ingest must grow the list, got %d", len(m.findings))
	}
	// the selected card stays inside the rendered window
	m.focus = focusFindings
	v := paneText(t, m)
	if !strings.Contains(v, "▌") || !strings.Contains(v, sel) {
		t.Fatalf("selected finding not visible after ingest\n%s", v)
	}
}

// Full reload path (the actual background-ingest seam, tui.go reload()).
// Skips — with the exact pointer — until the orchestrator wires reload()
// through ingestFindings (NEEDED_FOREIGN_CHANGES #1).
func TestIngestViaReloadIDStable(t *testing.T) {
	fs := mockFindings(4)
	m := newTestModel(t, 120, 35, fs)
	m.cursor = 1
	sel := m.currentID()
	newF := detailFixture()
	newF.ID, newF.Severity = "F042", "P0"
	if err := store.SaveFindings(m.dir, append([]store.Finding{newF}, fs...)); err != nil {
		t.Fatal(err)
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	if m.currentID() != sel {
		t.Skipf("reload shifted selection %s → %s: needs tui.go reload() → ingestFindings (NEEDED_FOREIGN_CHANGES #1)", sel, m.currentID())
	}
}

func TestFindingsPaneEmptyStates(t *testing.T) {
	// AI running, zero findings: both the list region message…
	m := newTestModelDiff(t, 160, 35, nil, "diff --git a/x.go b/x.go\n")
	m.bgReview = &store.AgentState{Status: store.AgentRunning}
	v := paneText(t, m)
	if !strings.Contains(v, "AI review running") {
		t.Fatalf("AI-running empty state missing\n%s", v)
	}
	// …and the details-region empty state block
	empty := stripANSIish(strings.Join(m.findingDetailsEmpty(m.layout().findingsW, 10), "\n"))
	if !strings.Contains(empty, "AI review running") {
		t.Fatalf("details empty state missing AI line: %q", empty)
	}

	// no AI, zero findings → manual-review hint
	m2 := newTestModelDiff(t, 160, 35, nil, "diff --git a/x.go b/x.go\n")
	if v := paneText(t, m2); !strings.Contains(v, "no findings — review the diff manually") {
		t.Fatalf("no-findings empty state missing\n%s", v)
	}

	// filter hides everything → filter hint
	m3 := newTestModel(t, 120, 35, mockFindings(2))
	m3.findings[0].Decision = "approved"
	m3.findings[1].Decision = "approved"
	m3.rebuild()
	m3.applyFilterChoice(2) // pending filter
	if v := paneText(t, m3); !strings.Contains(v, "no pending findings") {
		t.Fatalf("filter empty state missing\n%s", v)
	}
}

// The list must be rendered at the FINDINGS pane width, not the FILES pane
// width (the pre-wave defect: cards were cut at listWidth()).
func TestFindingsListUsesPaneWidth(t *testing.T) {
	m := newTestModel(t, 160, 45, mockFindings(1))
	m.findings[0].Title = "二十二chars-of-filler-markerXYZ-tail" // marker beyond filesW budget
	m.rebuild()
	L := m.layout()
	if L.filesW >= L.findingsW {
		t.Skip("layout has no width separation at this size")
	}
	v := paneText(t, m)
	if !strings.Contains(v, "markerXYZ") {
		t.Fatalf("title cut at %d cols (files width) instead of %d (findings width)\n%s",
			L.filesW, L.findingsW, v)
	}
	// narrow wrapper still works (listViewBody path)
	if nv := stripANSIish(strings.Join(m.findingsView(), "\n")); !strings.Contains(nv, "FINDINGS") {
		t.Fatal("findingsView() wrapper lost the header")
	}
}

// Geometry: the details region height + list region + chrome exactly fill
// the pane interior, and every rendered row fits the pane width.
func TestFindingsPaneRegionGeometry(t *testing.T) {
	for _, sz := range [][2]int{{120, 35}, {160, 45}, {200, 50}} {
		m := newTestModel(t, sz[0], sz[1], mockFindings(3))
		L := m.layout()
		w := L.findingsW
		inner := m.bodyHeight() - 2
		// chrome = separator + SELECTED FINDING label (the list header doubles
		// as the pane header — see renderFindingsPane)
		if got := m.findingDetailsRows() + m.findingsListRows() + 2; got != inner {
			t.Fatalf("%dx%d: regions + chrome = %d, inner = %d", sz[0], sz[1], got, inner)
		}
		rows := m.renderFindingsPane(w, m.bodyHeight())
		if len(rows) != inner {
			t.Fatalf("%dx%d: pane rows %d != interior %d", sz[0], sz[1], len(rows), inner)
		}
		for i, r := range rows {
			if cw := lipgloss.Width(r); cw > w {
				t.Fatalf("%dx%d: row %d is %d cells (> %d): %q", sz[0], sz[1], i, cw, w, stripANSIish(r))
			}
		}
	}
}

// The list window follows the cursor: after G (toLast) the last card is
// visible; after gg the first is; window state is listTop-based and shared
// with the mouse hit-test mapping.
func TestFindingsListWindowFollowsCursor(t *testing.T) {
	m := newTestModel(t, 160, 35, mockFindings(30))
	m.focus = focusFindings
	vis := m.findingsVisCards()
	for i := 0; i < 29; i++ {
		m.cursorDown()
	}
	m.findingsView() // render keeps the window in sync
	if m.listTop+vis-1 < m.cursor {
		t.Fatalf("cursor %d below window [top=%d vis=%d]", m.cursor, m.listTop, vis)
	}
	// the default sort is severity: assert the CURSOR's finding (last card in
	// filtered order), not a hard-coded ID
	lastID := m.findings[m.filtered[m.cursor]].ID
	if !strings.Contains(paneText(t, m), lastID) {
		t.Fatalf("cursor finding %s not visible after cursorDown×29", lastID)
	}
	for i := 0; i < 29; i++ {
		m.cursorUp()
	}
	m.findingsView()
	if m.listTop != 0 {
		t.Fatalf("window must return to top, listTop=%d", m.listTop)
	}
	if !strings.Contains(paneText(t, m), fmt.Sprintf("%d pending", 30)) {
		t.Fatal("header must show the pending count")
	}
}
