package tui

// Redesign scenario coverage (Layers 1–2, deterministic in-process):
//
//	C: hunk [/] and file {/} navigation across boundaries in the continuous
//	   diff; n/N search wrap.
//	D: a manual comment survives navigating to another file and back; the
//	   draft stays anchored at the right file:line:side.
//	E: selecting a finding updates the diff location WITHOUT stealing
//	   focus; approve/reject is reflected in the details STATUS pill;
//	   J/K scroll the details independently.
//	H: 80x24/100x30/120x35/160x45/200x50 — no overlapping panes, no row
//	   wider than the terminal, drafts survive resize.

import (
	"strings"
	"testing"

	"mr-review/internal/store"

	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// scenarioModel is the shared fixture: a real on-disk review session at
// wide size with the continuous diff focused (the startup contract).
func scenarioModel(t *testing.T) *model {
	t.Helper()
	return newSessionModel(t, writeReviewFixture(t, t.TempDir()))
}

// TestScenarioCHunkFileAndSearchWrap — Scenario C.
func TestScenarioCHunkFileAndSearchWrap(t *testing.T) {
	m := scenarioModel(t)
	const handler = "services/kent/handler.go"
	const worker = "services/kent/worker.go"

	// { at the very first file stays put (no wrap in file space)
	m = press(m, "{")
	if m.browseFile != handler {
		t.Fatalf("after { at the first file: browseFile = %q, want %q", m.browseFile, handler)
	}

	// } crosses the file boundary inside the continuous diff
	m = press(m, "}")
	if m.browseFile != worker {
		t.Fatalf("after }: browseFile = %q, want %q", m.browseFile, worker)
	}
	// the cursor lands on worker.go's first addressable row
	if row := m.browseRowAt(m.diffCursor); row == nil || row.file != worker || row.dl.NewNo == 0 {
		t.Fatalf("} landed on %+v, want an addressable worker.go row", m.browseRowAt(m.diffCursor))
	}

	// ] beyond the last hunk stays put with honest feedback
	m = press(m, "]")
	if m.msg != "last hunk" {
		t.Fatalf("] past the last hunk: msg = %q", m.msg)
	}

	// { back across the boundary, then [ finds the previous hunk header
	m = press(m, "{")
	if m.browseFile != handler {
		t.Fatalf("after { back: browseFile = %q, want %q", m.browseFile, handler)
	}
	m = press(m, "[")
	row := m.browseRowAt(m.diffCursor)
	if row == nil || row.dl.Kind != DiffHunkHeader {
		t.Fatalf("[ must land on a hunk header, got %+v", row)
	}

	// n/N wrap around the continuous document: exactly one match
	// ("w.cleanup(ctx)"), so forward AND backward jumps land on the same row
	m = press(m, "/", "c", "l", "e", "a", "n", "u", "p", "enter")
	if m.searchOn || m.searchText != "cleanup" {
		t.Fatalf("search commit: on=%v text=%q", m.searchOn, m.searchText)
	}
	if n := m.searchMatches(); n != 1 {
		t.Fatalf("searchMatches = %d, want 1", n)
	}
	match := m.diffCursor
	if r := m.browseRowAt(match); r == nil || !strings.Contains(r.dl.Text, "cleanup") {
		t.Fatalf("search landed on %+v, want the cleanup row", m.browseRowAt(match))
	}
	m = press(m, "j") // step off the match
	if m.diffCursor == match {
		t.Fatal("j must move off the match before the wrap check")
	}
	m = press(m, "n") // forward past the document end wraps to the match
	if m.diffCursor != match {
		t.Fatalf("n did not wrap forward: cursor %d, want %d", m.diffCursor, match)
	}
	m = press(m, "k") // step above the match
	m = press(m, "N") // backward past the document start wraps to the match
	if m.diffCursor != match {
		t.Fatalf("N did not wrap backward: cursor %d, want %d", m.diffCursor, match)
	}
}

// TestScenarioDCommentSurvivesFileNav — Scenario D.
func TestScenarioDCommentSurvivesFileNav(t *testing.T) {
	m := scenarioModel(t)
	const handler = "services/kent/handler.go"
	const worker = "services/kent/worker.go"

	// comment on the row under the startup cursor (handler.go, new side)
	anchor := m.browseRowAt(m.diffCursor)
	if anchor == nil || anchor.file != handler || anchor.dl.NewNo == 0 {
		t.Fatalf("startup cursor row = %+v", anchor)
	}
	line := anchor.dl.NewNo
	m = press(m, "c")
	if m.draftEdit == nil {
		t.Fatal("c must open the floating editor in the continuous diff")
	}
	if m.draftEdit.file != handler || m.draftEdit.side != "new" || m.draftEdit.line != line {
		t.Fatalf("editor anchored at %s:%d %s, want %s:%d new",
			m.draftEdit.file, m.draftEdit.line, m.draftEdit.side, handler, line)
	}
	m.draftEdit.ta.SetValue("anchor probe — survives navigation")
	m = press(m, "ctrl+s")
	if m.draftEdit != nil {
		t.Fatal("ctrl+s must close the editor")
	}

	// navigate to the other file and back — the draft must survive,
	// still anchored at handler.go:line (new side)
	m = press(m, "}")
	if m.browseFile != worker {
		t.Fatalf("nav to worker.go failed: %q", m.browseFile)
	}
	m = press(m, "{")
	if m.browseFile != handler {
		t.Fatalf("nav back failed: %q", m.browseFile)
	}
	cs, err := store.LoadComments(m.dir)
	if err != nil || len(cs) != 1 {
		t.Fatalf("comments on disk = %d, err %v, want the draft intact", len(cs), err)
	}
	if cs[0].File != handler || cs[0].Side != "new" || cs[0].Line != line {
		t.Errorf("draft re-anchored wrong: %s:%d %s, want %s:%d new",
			cs[0].File, cs[0].Line, cs[0].Side, handler, line)
	}
	if cs[0].Text != "anchor probe — survives navigation" {
		t.Errorf("draft text changed: %q", cs[0].Text)
	}
	// the gutter marker sits exactly on the anchor line
	if m.markerForLine(handler, "new", line) == "" {
		t.Errorf("no comment marker at %s:%d new", handler, line)
	}
	// and the comments navigator still lists it at the right location
	m.setView(viewComments)
	r := m.selectedRow()
	if r == nil || r.kind != "draft" || r.draft == nil {
		t.Fatalf("navigator row = %+v, want the draft", r)
	}
	if !strings.Contains(r.loc, "handler.go") {
		t.Errorf("navigator location = %q, want handler.go:…", r.loc)
	}
}

// TestScenarioEFindingDrivesDiffNoFocusSteal — Scenario E.
func TestScenarioEFindingDrivesDiffNoFocusSteal(t *testing.T) {
	m := scenarioModel(t)

	// 3 focuses findings (F001 selected); the center pane shows F001's diff
	m = press(m, "3")
	if m.focus != focusFindings {
		t.Fatalf("focus = %d, want findings", m.focus)
	}
	before := stripANSIish(strings.Join(m.detailPaneView(), "\n"))
	if !strings.Contains(before, "handler.go") {
		t.Fatal("center pane must show the selected finding's diff (F001 → handler.go)")
	}

	// selecting F002 moves the diff location WITHOUT stealing focus
	m = press(m, "j")
	if id := m.currentID(); id != "F002" {
		t.Fatalf("selection = %s, want F002", id)
	}
	if m.focus != focusFindings {
		t.Fatalf("selection stole focus: focus = %d, want findings", m.focus)
	}
	after := stripANSIish(strings.Join(m.detailPaneView(), "\n"))
	if !strings.Contains(after, "worker.go") {
		t.Fatal("the diff must follow the finding selection (F002 → worker.go)")
	}

	// approve is reflected in the SELECTED FINDING details STATUS pill
	m = press(m, "a")
	if f := findingByID(m.dir, "F002"); f == nil || f.Decision != "approved" {
		t.Fatalf("F002 decision = %v", f)
	}
	// auto-advance selected F001; walk back to F002 and read its STATUS
	m = press(m, "k")
	details := stripANSIish(strings.Join(m.renderFindingsPane(m.layout().findingsW, m.bodyHeight()), "\n"))
	if !strings.Contains(details, "STATUS") || !strings.Contains(details, "approved") {
		t.Errorf("details must show the approved STATUS pill, got:\n%s", details)
	}
	if !strings.Contains(details, "worker.go") {
		t.Errorf("details must show F002's location, got:\n%s", details)
	}

	// J/K scroll the details region; the list selection stays put
	sel := m.currentID()
	m.detailTop[fdScrollKey] = 0
	topBefore := m.detailTop[fdScrollKey]
	m = press(m, "J")
	if m.currentID() != sel {
		t.Fatalf("J moved the list selection: %s, want %s", m.currentID(), sel)
	}
	if m.detailTop[fdScrollKey] == topBefore {
		t.Fatal("J must scroll the finding details")
	}
	m = press(m, "K")
	if m.detailTop[fdScrollKey] != topBefore {
		t.Fatalf("K must scroll the details back: %d, want %d", m.detailTop[fdScrollKey], topBefore)
	}
}

// TestScenarioHLayoutGridsAndResizeDrafts — Scenario H: the five reference
// sizes render non-overlapping panes within the terminal, and a draft
// comment survives every resize.
func TestScenarioHLayoutGridsAndResizeDrafts(t *testing.T) {
	m := scenarioModel(t)

	// write a held draft first (floating editor over the diff)
	m = press(m, "c")
	if m.draftEdit == nil {
		t.Fatal("c must open the editor")
	}
	m.draftEdit.ta.SetValue("resize survivor")
	m = press(m, "ctrl+s")
	if len(m.comments) != 1 {
		t.Fatalf("draft not saved: %d comments", len(m.comments))
	}

	for _, size := range [][2]int{{80, 24}, {100, 30}, {120, 35}, {160, 45}, {200, 50}} {
		w, h := size[0], size[1]
		res, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		mm, ok := res.(*model)
		if !ok {
			t.Fatalf("%dx%d: Update returned %T", w, h, res)
		}
		m = mm
		lines := strings.Split(m.View().Content, "\n")

		// no row wider than the terminal
		for i, l := range lines {
			if cw := lipgloss.Width(l); cw > w {
				t.Fatalf("%dx%d: row %d is %d cells: %q", w, h, i, cw, visibleText(l))
			}
		}

		// pane structure per width band (no overlaps)
		top, cols, ok := plainCorners(lines)
		if !ok || len(cols) == 0 {
			t.Fatalf("%dx%d: no pane box found", w, h)
		}
		hdr := stripANSIish(lines[top+1])
		switch m.layout().mode {
		case layoutWide:
			if len(cols) != 3 {
				t.Fatalf("%dx%d: want 3 panes, got %d corners", w, h, len(cols))
			}
			iF := strings.Index(hdr, "FILES")
			iD := strings.Index(hdr, "DIFF")
			iN := strings.Index(hdr, "FINDINGS")
			if iF < 0 || iD < 0 || iN < 0 || !(iF < iD && iD < iN) {
				t.Fatalf("%dx%d: pane headers out of order: %q", w, h, hdr)
			}
			// each pane: 2 border cols; 1 gap col between panes (panesChrome(3)=8)
			fw, dw := cols[1]-cols[0]-3, cols[2]-cols[1]-3
			gw := lipgloss.Width(stripANSIish(lines[top])) - 2 - cols[2]
			if fw+dw+gw+8 != w || dw < 40 || fw < 26 || gw < 34 {
				t.Fatalf("%dx%d: pane widths %d+%d+%d+8 != %d (bounds fw≥26 dw≥40 gw≥34)", w, h, fw, dw, gw, w)
			}
			assertPaneEdges(t, lines, top, cols, w, h)
		case layoutMedium:
			if len(cols) != 2 {
				t.Fatalf("%dx%d: want 2 panes (sidebar+diff), got %d", w, h, len(cols))
			}
			if !strings.Contains(hdr, "FILES") || !strings.Contains(hdr, "DIFF") || strings.Contains(hdr, "FINDINGS") {
				t.Fatalf("%dx%d: medium header wrong: %q", w, h, hdr)
			}
			assertPaneEdges(t, lines, top, cols, w, h)
		default: // narrow — a single focused pane
			if len(cols) != 1 {
				t.Fatalf("%dx%d: want 1 pane, got %d", w, h, len(cols))
			}
		}

		// drafts survive the resize (memory + disk)
		if len(m.comments) != 1 || m.comments[0].Text != "resize survivor" {
			t.Fatalf("%dx%d: draft lost in memory: %+v", w, h, m.comments)
		}
		if cs, err := store.LoadComments(m.dir); err != nil || len(cs) != 1 || cs[0].Text != "resize survivor" {
			t.Fatalf("%dx%d: draft lost on disk: %d, %v", w, h, len(cs), err)
		}
		// selection survives
		if m.currentID() != "F001" {
			t.Fatalf("%dx%d: selection lost: %s", w, h, m.currentID())
		}
	}
}

// plainCorners is the in-process twin of vtCorners: the top pane-border row
// and the columns of its "╭" corners (one per bordered pane box).
func plainCorners(lines []string) (top int, cols []int, ok bool) {
	for y, l := range lines {
		p := stripANSIish(l)
		if !strings.Contains(p, "╭") {
			continue
		}
		n := 0 // CELL index (box-drawing runes are 3 bytes — byte index lies)
		for _, r := range p {
			if r == '╭' {
				cols = append(cols, n)
			}
			n++
		}
		return y, cols, true
	}
	return -1, nil, false
}

// assertPaneEdges is the in-process twin of vtAssertPaneEdges.
func assertPaneEdges(t *testing.T, lines []string, top int, cols []int, w, h int) {
	t.Helper()
	bottom := -1
	for y := top + 1; y < len(lines); y++ {
		if strings.Contains(stripANSIish(lines[y]), "╰") {
			bottom = y
			break
		}
	}
	if bottom < 0 {
		t.Fatalf("%dx%d: bottom border missing below row %d", w, h, top)
	}
	for y := top + 1; y < bottom; y++ {
		plain := []rune(stripANSIish(lines[y]))
		for _, cx := range cols {
			if cx >= len(plain) || plain[cx] != '│' {
				t.Fatalf("%dx%d: pane edge broken at row %d col %d (corners %v): %q", w, h, y, cx, cols, visibleText(lines[y]))
			}
		}
	}
}
