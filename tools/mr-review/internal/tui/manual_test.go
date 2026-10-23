package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"mr-review/internal/store"
)

// multiFileDiff covers several files with different statuses.
func multiFileDiff() string {
	return "diff --git a/services/x/handler.go b/services/x/handler.go\n" +
		"--- a/services/x/handler.go\n+++ b/services/x/handler.go\n" +
		"@@ -10,4 +10,5 @@ func handler() {\n" +
		" \tprior()\n" +
		"+\tmarkOffering(ctx, target)\n" +
		" \tfollowing()\n" +
		"diff --git a/services/y/service.go b/services/y/service.go\n" +
		"--- a/services/y/service.go\n+++ b/services/y/service.go\n" +
		"@@ -20,3 +20,3 @@ func service() {\n" +
		"-oldCall()\n" +
		"+newCall()\n" +
		" \tfollowing()\n" +
		"diff --git a/services/z/deleted.go b/services/z/deleted.go\n" +
		"deleted file mode 100644\n" +
		"--- a/services/z/deleted.go\n+++ /dev/null\n" +
		"@@ -1,2 +0,0 @@\n" +
		"-gone()\n"
}

func newMultiFileModel(t *testing.T, w, h int) *model {
	t.Helper()
	fs := mockFindings(2)
	fs[0].File = "services/x/handler.go"
	fs[1].File = "services/y/service.go"
	return newTestModelDiff(t, w, h, fs, multiFileDiff())
}

func tap(m *model, s string) *model {
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: s})
	return m2.(*model)
}

// ---- file tree -------------------------------------------------------------

func TestFileTreeBuildAndStatus(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.buildTree()
	paths := m.changedFiles()
	if len(paths) != 3 {
		t.Fatalf("expected 3 changed files, got %v", paths)
	}
	if got := m.fileStatus("services/x/handler.go"); got != 'M' {
		t.Fatalf("handler.go status = %q (want M)", got)
	}
	if got := m.fileStatus("services/y/service.go"); got != 'M' {
		t.Fatalf("service.go status = %q (want M)", got)
	}
	if got := m.fileStatus("services/z/deleted.go"); got != 'D' {
		t.Fatalf("deleted.go status = %q (want D)", got)
	}
	// tree shows the directory hierarchy
	rows := m.treeRows()
	if !rows[0].node.dir || rows[0].node.name != "services" {
		t.Fatalf("first tree row must be the services dir, got %+v", rows[0].node)
	}
}

func TestFileTreeViewAndCounts(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.setView(viewFiles)
	v := m.View().Content
	if !strings.Contains(v, "CHANGED FILES 0/3") {
		t.Fatalf("files header missing: %s", v[:200])
	}
	if !strings.Contains(v, "services/") {
		t.Fatal("directory rows missing")
	}
	if !strings.Contains(v, "1✦") {
		t.Fatal("finding counts missing (compact chip expected)")
	}
	// reviewed marks appear after toggling
	press(m, "2") // ensure files view (already there)
	m.treeCursor = 0
	// select the first file row (services dir) then a file
	for i, r := range m.treeRows() {
		if !r.node.dir && strings.HasSuffix(r.node.path, "handler.go") {
			m.treeCursor = i
			break
		}
	}
	m.toggleReviewedFile()
	v2 := m.View().Content
	if !strings.Contains(v2, "CHANGED FILES 1/3") || !strings.Contains(v2, "✓") {
		t.Fatal("reviewed mark missing after toggle")
	}
}

func TestFileReviewedPersists(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.setView(viewFiles)
	for i, r := range m.treeRows() {
		if !r.node.dir && strings.HasSuffix(r.node.path, "service.go") {
			m.treeCursor = i
			break
		}
	}
	m.toggleReviewedFile()
	// reload from disk
	rev2, err := store.LoadReview(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev2.ReviewedFiles) != 1 || !strings.HasSuffix(rev2.ReviewedFiles[0], "service.go") {
		t.Fatalf("reviewed file not persisted: %+v", rev2.ReviewedFiles)
	}
	// toggling again removes it
	m.toggleReviewedFile()
	rev3, _ := store.LoadReview(m.dir)
	if len(rev3.ReviewedFiles) != 0 {
		t.Fatalf("toggle-off failed: %+v", rev3.ReviewedFiles)
	}
}

func TestNextUnreviewedFile(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.setView(viewFiles)
	m.browseFile = "services/x/handler.go"
	m.nextUnreviewed()
	if m.browseFile != "services/y/service.go" && m.treeSelection() != "services/y/service.go" {
		t.Fatalf("next unreviewed wrong: browse=%q sel=%q", m.browseFile, m.treeSelection())
	}
	// everything reviewed → message, no crash
	rev := m.rev
	rev.ReviewedFiles = m.changedFiles()
	m.nextUnreviewed()
	if !strings.Contains(m.msg, "all changed files reviewed") {
		t.Fatalf("expected completion message, got %q", m.msg)
	}
}

// ---- browse mode / continuous diff ----------------------------------------

func TestBrowseModeOpenAndCursor(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	// gd from a finding opens the continuous diff at its line
	m2, _ := m.Update(key("gd"))
	mm := m2.(*model)
	if mm.browseFile == "" {
		t.Fatal("gd must open the continuous diff")
	}
	if mm.browseFile != "services/x/handler.go" {
		t.Fatalf("wrong browse file: %q", mm.browseFile)
	}
	if mm.focus != focusDiff || mm.tab != tabOverview {
		t.Fatal("gd must focus the overview tab")
	}
	v := mm.View().Content
	if !strings.Contains(v, "markOffering") {
		t.Fatal("browse view must render the file diff")
	}
	// j/k move the cursor within the file (from a non-edge position)
	cur := mm.diffCursor
	tap(mm, "k")
	tap(mm, "k")
	if mm.diffCursor != cur-2 {
		t.Fatalf("k must move the diff cursor up: %d → %d", cur, mm.diffCursor)
	}
	tap(mm, "j")
	if mm.diffCursor != cur-1 {
		t.Fatalf("j must move the diff cursor down: %d → %d", cur-2, mm.diffCursor)
	}
	// esc returns to findings mode
	tap(mm, "esc")
	if mm.browseFile != "" {
		t.Fatal("esc must exit browse mode")
	}
}

func TestFileJumpAndHunkJumpBrowse(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.browseFilePath("services/x/handler.go")
	// } → next file
	press(m, "}")
	if m.browseFile != "services/y/service.go" {
		t.Fatalf("} must move to the next file, got %q", m.browseFile)
	}
	// { → back
	press(m, "{")
	if m.browseFile != "services/x/handler.go" {
		t.Fatalf("{ must move to the previous file, got %q", m.browseFile)
	}
	// ] jumps to the next hunk header
	m.browseFilePath("services/y/service.go")
	hunkRow := -1
	for i, r := range m.browseDoc() {
		if !r.header && r.dl.Kind == DiffHunkHeader {
			hunkRow = i
			break
		}
	}
	m.diffCursor = 0
	press(m, "]")
	if m.diffCursor != hunkRow {
		t.Fatalf("] must land on the hunk header %d, got %d", hunkRow, m.diffCursor)
	}
}

func TestHunkReviewedTracking(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.browseFilePath("services/x/handler.go")
	m.diffCursor = m.firstAddressableRow("services/x/handler.go")
	m.toggleReviewedHunk()
	if len(m.rev.ReviewedHunks) != 1 || !strings.HasPrefix(m.rev.ReviewedHunks[0], "services/x/handler.go|") {
		t.Fatalf("hunk key wrong: %+v", m.rev.ReviewedHunks)
	}
	rev2, _ := store.LoadReview(m.dir)
	if len(rev2.ReviewedHunks) != 1 {
		t.Fatal("hunk review not persisted")
	}
	// mark shows in the rendering
	v := m.View().Content
	if !strings.Contains(v, "✓") {
		t.Fatal("reviewed hunk mark missing")
	}
}

// ---- visual range + manual comments ---------------------------------------

func TestVisualRangeComment(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.browseFilePath("services/x/handler.go")
	start := m.diffCursor
	press(m, "V") // start visual
	press(m, "j") // extend
	press(m, "j") // extend again
	if !m.visualOn {
		t.Fatal("V must start the selection")
	}
	lo, hi := m.visualRange()
	if hi-lo != 2 {
		t.Fatalf("selection must span 3 rows, got %d..%d", lo, hi)
	}
	// c opens the range draft editor
	press(m, "c")
	if m.draftEdit == nil {
		t.Fatal("c must open the draft editor")
	}
	if m.draftEdit.lineEnd != m.draftEdit.line+2 {
		t.Fatalf("range metadata wrong: %d-%d", m.draftEdit.line, m.draftEdit.lineEnd)
	}
	if m.draftEdit.file != "services/x/handler.go" || m.draftEdit.side != "new" {
		t.Fatalf("draft location wrong: %+v", m.draftEdit)
	}
	_ = start
	// type + save
	m.draftEdit.ta.SetValue("Check this range please")
	press(m, "ctrl+s")
	if m.draftEdit != nil {
		t.Fatal("ctrl+s must close the editor")
	}
	if len(m.comments) != 1 || m.comments[0].LineEnd-m.comments[0].Line != 2 {
		t.Fatalf("draft not saved with range: %+v", m.comments)
	}
	if m.comments[0].Origin != "human" || m.comments[0].State != store.CommentDraft {
		t.Fatalf("draft origin/state wrong (save must start held, not ready): %+v", m.comments[0])
	}
	// persisted
	cs, _ := store.LoadComments(m.dir)
	if len(cs) != 1 || cs[0].Text != "Check this range please" {
		t.Fatal("draft not persisted")
	}
}

func TestSingleLineCommentOnDiffRow(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.browseFilePath("services/y/service.go")
	// move to the deleted line (old side)
	for i, r := range m.browseDoc() {
		if !r.header && r.dl.Kind == DiffDel {
			m.diffCursor = i
			break
		}
	}
	press(m, "c")
	if m.draftEdit == nil {
		t.Fatal("c must open the editor")
	}
	if m.draftEdit.side != "old" {
		t.Fatalf("deleted-line comment must target the old side, got %q", m.draftEdit.side)
	}
	m.draftEdit.ta.SetValue("why was this removed?")
	press(m, "ctrl+s")
	if m.comments[0].Side != "old" || m.comments[0].Line == 0 {
		t.Fatalf("old-side draft wrong: %+v", m.comments[0])
	}
}

func TestGeneralCommentViaPalette(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.openPalette()
	m.paletteInput.SetValue("general")
	m.refreshPalette()
	var foundIdx = -1
	for i, it := range m.paletteItems {
		if it.ID == "comments.general" {
			foundIdx = i
			break
		}
	}
	if foundIdx < 0 {
		t.Fatalf("comments.general action missing from palette: %v", m.paletteItems)
	}
	// Select comments.general directly via the palette's own cursor (press j
	// outside the palette navigates the findings list, not the palette).
	m.paletteSel = foundIdx
	m2, _ := m.Update(keyEnter)
	mm := m2.(*model)
	if mm.draftEdit == nil || !mm.draftEdit.general {
		t.Fatal("palette must open the general comment editor")
	}
	mm.draftEdit.ta.SetValue("overall: nice work")
	tap(mm, "ctrl+s")
	if !mm.comments[0].General {
		t.Fatal("general draft flag wrong")
	}
}

func TestCommentNavigatorLifecycle(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	// seed two drafts
	m.comments = []store.DraftComment{
		store.NewDraftComment("C001", "services/x/handler.go", "new", 11, 11, false, "first"),
		store.NewDraftComment("C002", "", "", 0, 0, true, "second"),
	}
	m.setView(viewComments)
	v := m.View().Content
	if !strings.Contains(v, "COMMENTS") || !strings.Contains(v, "first") {
		t.Fatal("navigator rows missing")
	}
	// edit: e opens the editor with the draft text
	m.editSelectedDraft()
	if m.draftEdit == nil || m.draftEdit.id != "C001" || m.draftEdit.origText == "" && m.draftEdit.ta.Value() == "" {
		t.Fatalf("edit must preload the draft (id=%v)", m.draftEdit)
	}
	m.draftEdit.ta.SetValue("first (edited)")
	press(m, "ctrl+s")
	if m.comments[0].Text != "first (edited)" {
		t.Fatalf("edit not saved: %+v", m.comments[0])
	}
	// toggle ready → held
	m.toggleDraftReady()
	if m.comments[0].State != store.CommentDraft {
		t.Fatal("toggle ready must hold the draft back")
	}
	v = m.View().Content
	if !strings.Contains(v, "held") {
		t.Fatal("held state missing in the navigator")
	}
	// delete with confirmation: d then y
	press(m, "d")
	if !m.confirmDelete {
		t.Fatal("d must ask for confirmation")
	}
	press(m, "y")
	if len(m.comments) != 1 {
		t.Fatalf("draft not deleted: %d left", len(m.comments))
	}
	cs, _ := store.LoadComments(m.dir)
	if len(cs) != 1 {
		t.Fatal("deletion not persisted")
	}
	// esc cancels the confirmation without deleting
	m.comments = append(m.comments, store.NewDraftComment("C009", "", "", 0, 0, true, "x"))
	m.confirmDelete = true
	press(m, "n")
	if len(m.comments) != 2 {
		t.Fatal("n must keep the draft")
	}
}

func TestCommentMarkersInDiffGutter(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.comments = []store.DraftComment{
		store.NewDraftComment("C001", "services/x/handler.go", "new", 11, 11, false, "note"),
	}
	m.browseFilePath("services/x/handler.go")
	if got := m.markerForLine("services/x/handler.go", "new", 11); got != "●" {
		t.Fatalf("marker = %q", got)
	}
	if got := m.markerForLine("services/x/handler.go", "new", 12); got != "" {
		t.Fatalf("adjacent line must have no marker, got %q", got)
	}
	// multiple comments collapse to a count
	m.comments = append(m.comments, store.NewDraftComment("C002", "services/x/handler.go", "new", 11, 11, false, "two"))
	if got := m.markerForLine("services/x/handler.go", "new", 11); got != "2" {
		t.Fatalf("count marker = %q", got)
	}
}

// ---- focus / view switching -----------------------------------------------

func TestViewSwitchingAndFocusRestore(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	// 1/2/3 focus the panes: 1 = files, 2 = diff, 3 = findings
	press(m, "1")
	if m.leftView != viewFiles {
		t.Fatal("1 must show the file tree")
	}
	press(m, "3")
	if m.leftView != viewFindings || m.focus != focusFindings {
		t.Fatal("3 must show the findings pane")
	}
	press(m, "2")
	if m.focus != focusDiff {
		t.Fatal("2 must focus the diff pane")
	}
	// ctrl+h/l move focus between panes without changing the selection
	m.cursor = 1
	press(m, "ctrl+h")
	if m.focus != focusFiles {
		t.Fatal("ctrl+h must focus the files pane")
	}
	if m.cursor != 1 {
		t.Fatal("focus change must not move the selection")
	}
	press(m, "ctrl+l")
	if m.focus != focusDiff {
		t.Fatal("ctrl+l must focus the diff pane")
	}
}

func TestZoomPreservesState(t *testing.T) {
	m := newMultiFileModel(t, 160, 45)
	m.cursor = 1
	m.tab = tabOverview
	press(m, "2") // zoom maximizes the FOCUSED pane: focus the diff first
	press(m, "z")
	if !m.zoom {
		t.Fatal("z must zoom")
	}
	v := m.View().Content
	if !strings.Contains(v, "● DIFF") || strings.Contains(v, "● FILES") || strings.Contains(v, "● FINDINGS") {
		t.Fatal("zoom must show only the focused (diff) pane")
	}
	if m.cursor != 1 || m.tab != tabOverview {
		t.Fatal("zoom must preserve selection and tab")
	}
	press(m, "z")
	if m.zoom {
		t.Fatal("z must toggle zoom off")
	}
}

func TestInspectorOverlay(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.focus = focusFindings
	press(m, "i")
	if m.overlay != ovInspector {
		t.Fatal("i must open the inspector")
	}
	v := m.View().Content
	sv := stripANSIish(v) // the actions footer separates segments with styled " · "
	// the floating view renders the full details-region format (formatted
	// explanation, status pill, actions) at a large window size
	for _, want := range []string{"F001", "P2", "STATUS", "pending", "wrong order", "EVIDENCE", "a approve · r reject"} {
		if !strings.Contains(sv, want) && !strings.Contains(strings.ToLower(sv), strings.ToLower(want)) {
			t.Fatalf("inspector missing %q", want)
		}
	}
	// esc closes without touching state
	press(m, "esc")
	if m.overlay != ovNone {
		t.Fatal("esc must close the inspector")
	}
	if m.curMode() != modeNormal {
		t.Fatal("inspector close must restore normal mode")
	}
}

func TestSearchFilterAcrossViews(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	// findings: / filters live
	press(m, "/")
	if m.curMode() != modeSearch {
		t.Fatal("/ must open search")
	}
	m.searchInput.SetValue("F002")
	m.searchText = "F002"
	m.applyViewFilters()
	if len(m.filtered) != 1 || m.findings[m.filtered[0]].ID != "F002" {
		t.Fatalf("findings filter wrong: %d", len(m.filtered))
	}
	// esc clears
	press(m, "esc")
	if m.searchText != "" || len(m.filtered) != 2 {
		t.Fatal("esc must clear the search")
	}
}

func TestFindingToDiffAndBack(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.focus = focusFindings
	// enter → details: jumps the diff AND opens the floating finding view
	press(m, "enter")
	if m.focus != focusDiff {
		t.Fatal("enter must focus details")
	}
	if m.topOverlay() != ovInspector {
		t.Fatal("enter must open the floating finding view")
	}
	if !strings.Contains(stripANSIish(m.View().Content), "F001") {
		t.Fatal("floating view must show the finding")
	}
	press(m, "esc") // close the floating view; diff location persists
	if m.topOverlay() != ovNone {
		t.Fatal("esc must close the floating view")
	}
	// gd → browse the finding's location
	press(m, "gd")
	if m.browseFile != "services/x/handler.go" {
		t.Fatalf("gd wrong file: %q", m.browseFile)
	}
	row := m.browseDoc()[m.diffCursor]
	if row.header {
		t.Fatal("gd must land on a diff row, not a file header")
	}
	line := row.dl
	if line.NewNo == 0 {
		t.Fatalf("gd must land near the finding line, cursor row %d", m.diffCursor)
	}
	// esc → back to findings, selection preserved
	press(m, "esc")
	if m.browseFile != "" {
		t.Fatal("esc must exit browse")
	}
	if m.currentID() != "F001" {
		t.Fatalf("selection lost: %q", m.currentID())
	}
}

// TestContinuousDiffAcrossFiles — the whole MR is one document: the cursor
// walks out of file A into file B, browseFile follows, { } jump between file
// headers, and a comment lands on the right file:line.
func TestContinuousDiffAcrossFiles(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.browseFilePath("services/x/handler.go")
	// walk to the very end of the document (into services/z/deleted.go)
	for i := 0; i < 100; i++ {
		tap(m, "j")
	}
	if m.browseFile != "services/z/deleted.go" {
		t.Fatalf("cursor must reach the last file, got %q", m.browseFile)
	}
	// comment on the deleted line at the document end
	tap(m, "c")
	if m.draftEdit == nil {
		t.Fatal("c must open the editor at the document end")
	}
	if m.draftEdit.file != "services/z/deleted.go" || m.draftEdit.side != "old" {
		t.Fatalf("comment location wrong: %+v", m.draftEdit)
	}
	tap(m, "esc") // discard

	// { } jump between file sections
	m2 := newMultiFileModel(t, 120, 35)
	m2.browseFilePath("services/y/service.go")
	tap(m2, "{")
	if m2.browseFile != "services/x/handler.go" {
		t.Fatalf("{ must reach the previous file, got %q", m2.browseFile)
	}
	tap(m2, "}")
	tap(m2, "}")
	if m2.browseFile != "services/z/deleted.go" {
		t.Fatalf("}} must reach the last file, got %q", m2.browseFile)
	}

	// search across the whole document
	m3 := newMultiFileModel(t, 120, 35)
	m3.browseFilePath("services/x/handler.go")
	m3.searchText = "newCall" // only exists in services/y/service.go
	m3.focus = focusDiff
	m3.searchJump(1)
	if m3.browseFile != "services/y/service.go" {
		t.Fatalf("diff search must cross files, got %q", m3.browseFile)
	}
}
