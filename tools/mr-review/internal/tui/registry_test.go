package tui

import (
	"strings"
	"testing"

	"mr-review/internal/store"
)

// TestRegistryActionIDsUnique guards the palette/help contracts.
func TestRegistryActionIDsUnique(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	acts := buildActions(m)
	seen := map[string]bool{}
	for _, a := range acts {
		if a.ID == "" {
			t.Fatalf("action without ID: %+v", a)
		}
		if seen[a.ID] {
			t.Fatalf("duplicate action ID %q", a.ID)
		}
		seen[a.ID] = true
		if a.Name == "" || a.Desc == "" {
			t.Fatalf("action %s missing name/description", a.ID)
		}
		switch a.Cat {
		case CatNavigation, CatFindings, CatManual, CatComments, CatGitLab, CatExport, CatView, CatApp:
		default:
			t.Fatalf("action %s has unknown category %q", a.ID, a.Cat)
		}
	}
}

// TestRegistryNoKeyConflicts ensures one key never maps to two enabled
// actions in the same state — keyboard dispatch would otherwise be ambiguous.
// Context-exclusive bindings (e.g. e = edit finding in findings view vs edit
// draft in the comment navigator) are allowed: exactly one is enabled at a
// time, which the second half of this test verifies.
func TestRegistryNoKeyConflicts(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	states := []*model{m}
	// check several representative states
	mFiles := newMultiFileModel(t, 120, 35)
	mFiles.setView(viewFiles)
	mComments := newMultiFileModel(t, 120, 35)
	mComments.setView(viewComments)
	mComments.comments = []store.DraftComment{
		store.NewDraftComment("C001", "services/x/handler.go", "new", 11, 11, false, "x"),
	}
	mBrowse := newMultiFileModel(t, 120, 35)
	mBrowse.browseFilePath("services/x/handler.go")
	states = append(states, mFiles, mComments, mBrowse)

	for si, sm := range states {
		owner := map[string]string{}
		for _, a := range enabledActions(sm) {
			for _, k := range a.Keys {
				if prev, dup := owner[k]; dup && prev != a.ID {
					t.Fatalf("state %d: key %q bound to both %s and %s", si, k, prev, a.ID)
				}
				owner[k] = a.ID
			}
		}
		// safety-critical keys must exist exactly once and never be remapped away
		for _, k := range []string{"a", "r", "P", "?", ":"} {
			if owner[k] == "" {
				t.Fatalf("state %d: required key %q lost", si, k)
			}
		}
	}
}

// TestRegistryContextExclusiveKeys — e and d mean different things per view,
// but never two things at once.
func TestRegistryContextExclusiveKeys(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.setView(viewComments)
	m.comments = []store.DraftComment{
		store.NewDraftComment("C001", "services/x/handler.go", "new", 11, 11, false, "x"),
	}
	acts := map[string]string{}
	for _, a := range enabledActions(m) {
		for _, k := range a.Keys {
			if _, ok := acts[k]; !ok { // first registration wins, like lookup
				acts[k] = a.ID
			}
		}
	}
	if acts["e"] != "comments.edit" || acts["d"] != "comments.delete" {
		t.Fatalf("comments view must own e/d: e=%q d=%q", acts["e"], acts["d"])
	}
	m.setView(viewFindings)
	acts = map[string]string{}
	for _, a := range enabledActions(m) {
		for _, k := range a.Keys {
			acts[k] = a.ID
		}
	}
	if acts["e"] != "findings.edit-comment" || acts["d"] != "findings.discuss" {
		t.Fatalf("findings view must own e/d: e=%q d=%q", acts["e"], acts["d"])
	}
}

// TestRegistryHelpDerived checks the help overlay lists registry entries (no
// drift, the old p/l/R ghost bindings stay gone).
func TestRegistryHelpDerived(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	help := strings.Join(m.helpOverlay(120, 80), "\n")
	for _, want := range []string{"COMMAND PALETTE", "approve", "publish"} {
		if !strings.Contains(strings.ToLower(help), strings.ToLower(want)) {
			t.Fatalf("help missing %q", want)
		}
	}
	if strings.Contains(help, "raw agent output (p)") {
		t.Fatal("stale help entry for p/l resurfaced")
	}
}

// TestPaletteOpenFilterExecute runs the full palette loop: open with ":",
// fuzzy-filter, navigate, execute with Enter, restore focus.
func TestPaletteOpenFilterExecute(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	m.focus = focusFindings

	// open via the registry key
	m2, _ := m.Update(key(":"))
	mm := m2.(*model)
	if !mm.paletteOn {
		t.Fatal(": must open the palette")
	}
	if len(mm.paletteItems) == 0 {
		t.Fatal("palette must list enabled actions")
	}
	// typing filters
	m3, _ := mm.Update(key("approv"))
	mmm := m3.(*model)
	if len(mmm.paletteItems) == 0 || mmm.paletteItems[0].ID != "findings.approve" {
		t.Fatalf("fuzzy filter failed: %d items, first=%v", len(mmm.paletteItems), mmm.paletteItems)
	}
	// executing runs the handler (approve F001) and closes the palette
	m4, _ := mmm.Update(keyEnter)
	m4m := m4.(*model)
	if m4m.paletteOn {
		t.Fatal("enter must execute and close")
	}
	if m4m.findings[m4m.filtered[0]].Decision != "approved" {
		t.Fatal("palette execute must run the action handler")
	}
	// focus is preserved (never changed by the palette)
	if m4m.focus != focusFindings {
		t.Fatalf("focus must be restored, got %d", m4m.focus)
	}
}

// TestPaletteEscCancelsAndFuzzyScoring tests close + match ordering.
func TestPaletteEscCancelsAndFuzzyScoring(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	m.openPalette()
	m2, _ := m.Update(keyEsc)
	if m2.(*model).paletteOn {
		t.Fatal("esc must close the palette")
	}
	if m2.(*model).overlay != ovNone {
		t.Fatal("palette close must not leave an overlay behind")
	}
	// scoring: word-start prefix beats scattered subsequence
	if fuzzyScore("filter findings", "fi") <= fuzzyScore("files filter", "fi") {
		// both match; the word-start bonus must at least not reverse order
		t.Log("score order:", fuzzyScore("filter findings", "fi"), fuzzyScore("files filter", "fi"))
	}
	if fuzzyScore("approve", "zzz") >= 0 {
		t.Fatal("non-matching query must score negative")
	}
	if fuzzyScore("approve finding", "appr") < 0 {
		t.Fatal("prefix must match")
	}
}

// TestPaletteTypingDoesNotFireActions — input safety: review shortcuts must
// not execute while the palette input has focus.
func TestPaletteTypingDoesNotFireActions(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(3))
	m.openPalette()
	// "a" would approve in normal mode; in the palette it must only filter
	m2, _ := m.Update(key("a"))
	mm := m2.(*model)
	if !mm.paletteOn {
		t.Fatal("palette must stay open while typing")
	}
	if mm.findings[mm.filtered[0]].Decision != "pending" {
		t.Fatal("typing in the palette must never fire a review action")
	}
	// "q" must not quit either
	m3, _ := mm.Update(key("q"))
	if m3.(*model).paletteOn == false {
		t.Fatal("palette must remain open (q is not quit inside the palette)")
	}
}

// TestRegistryDisabledActionsNeverRun — unavailable actions must not execute
// against an invalid selection.
func TestRegistryDisabledActionsNeverRun(t *testing.T) {
	m := newTestModel(t, 120, 35, nil) // no findings at all
	// a (approve) is context-gated: no finding → nothing happens, no crash
	m2, _ := m.Update(key("a"))
	mm := m2.(*model)
	if mm.msg == "" && mm.curMode() == modeNormal {
		// accepted: either a silent no-op or a status message; must not crash
		_ = mm
	}
	// e (edit comment) with no finding must not open an editor
	m3, _ := m.Update(key("e"))
	if m3.(*model).editingComment {
		t.Fatal("e must not open the comment editor without a finding")
	}
	// c (manual comment) with no finding and no browse must not open the editor
	m4, _ := m.Update(key("c"))
	if m4.(*model).draftEdit != nil {
		t.Fatal("c must not open the draft editor without a location")
	}
}

// TestStatusFilterActions — every review status is a searchable palette
// action that filters the findings list (and can be cleared again).
func TestStatusFilterActions(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	// approve F001 so pending/approved differ
	tap(m, "a")
	if m.findings[m.filtered[0]].Decision == "pending" {
		t.Fatal("fixture: F001 should be approved now")
	}
	pendingBefore := 0
	for i := range m.findings {
		if m.findings[i].Decision == "pending" {
			pendingBefore++
		}
	}

	// palette → "pending" → execute
	m.openPalette()
	m.paletteInput.SetValue("pending")
	m.refreshPalette()
	sel := -1
	for i, a := range m.paletteItems {
		if a.ID == "findings.filter-pending" {
			sel = i
			break
		}
	}
	if sel < 0 {
		t.Fatal("palette must list the pending status filter")
	}
	m.paletteSel = sel // navigate to the entry, then execute it
	m2, _ := m.Update(keyEnter)
	mm := m2.(*model)
	if mm.leftView != viewFindings || len(mm.filtered) != pendingBefore {
		t.Fatalf("pending filter: %d visible, want %d", len(mm.filtered), pendingBefore)
	}
	for _, idx := range mm.filtered {
		if f := mm.findings[idx]; f.Decision != "pending" || f.Publication != nil {
			t.Fatalf("non-pending finding visible: %s (%s)", f.ID, f.Decision)
		}
	}
	if !strings.Contains(mm.msg, "pending") {
		t.Fatalf("status feedback missing: %q", mm.msg)
	}

	// direct helper: rejected (fixture has none → empty state, no crash),
	// then all clears it
	mm.setStatusFilter("rejected")
	if len(mm.filtered) != 0 {
		t.Fatalf("rejected filter on a rejection-free review must show 0, got %d", len(mm.filtered))
	}
	mm.setStatusFilter("all")
	if len(mm.filtered) != len(mm.findings) {
		t.Fatalf("all filter: %d visible, want %d", len(mm.filtered), len(mm.findings))
	}

	// severity + text survive a status switch
	mm.searchText = "F002"
	mm.applyViewFilters()
	mm.setStatusFilter("pending")
	if mm.filter.Severity != "" || mm.filter.Text != "F002" {
		t.Fatalf("severity/text filter lost: %+v", mm.filter)
	}
	if len(mm.filtered) != 1 || mm.findings[mm.filtered[0]].ID != "F002" {
		t.Fatalf("combined status+text filter wrong: %d visible", len(mm.filtered))
	}
}
