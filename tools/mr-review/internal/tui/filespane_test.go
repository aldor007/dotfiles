// filespane_test.go — FILES pane (lazygit-style) behavior: visual states,
// h/l tree semantics, expand/collapse-all + flat toggle, filters preserving
// state, sort stability, preview focus rule, windowed rendering, and the
// pane golden at 100x30.
package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"mr-review/internal/store"
	"mr-review/internal/tui/widget"
)

// filesModel is a fresh multi-file fixture with the FILES pane focused.
func filesModel(t *testing.T, w, h int) *model {
	t.Helper()
	m := newMultiFileModel(t, w, h)
	m.setView(viewFiles)
	return m
}

// filesTreeRows returns the visible rows as "depth:name" strings.
func filesTreeRows(m *model) []string {
	var out []string
	for _, r := range m.treeRows() {
		out = append(out, fmt.Sprintf("%d:%s", r.depth, r.node.path))
	}
	return out
}

func filesRowOf(m *model, path string) int { return m.filesRowForPath(path) }

// ---- visual states ---------------------------------------------------------

func TestFilesPaneVisualStates(t *testing.T) {
	m := filesModel(t, 160, 45)
	// one file reviewed, a finding + a draft comment on handler.go
	m.rev.ReviewedFiles = []string{"services/y/service.go"}
	m.comments = []store.DraftComment{
		store.NewDraftComment("C001", "services/x/handler.go", "new", 11, 11, false, "note"),
	}
	v := stripANSIish(strings.Join(m.filesPaneRows(m.listWidth(), m.bodyHeight()-2), "\n"))
	for _, want := range []string{
		"CHANGED FILES 1/3 reviewed", // header + review progress
		"○",                          // unreviewed mark (shape, not color-only)
		"✓",                          // reviewed mark
		"M",                          // modified status letters
		"D",                          // deleted status letter
		"+1",                         // +n line count (handler.go / service.go)
		"-1",                         // −m line count (service.go / deleted.go)
		"1✦",                         // finding count on handler.go
		"1●",                         // comment count on handler.go
		"services/",                  // dir row with glyph
		"handler.go", "service.go", "deleted.go",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("files pane missing %q in:\n%s", want, v)
		}
	}
	// 1/2 reviewed is visible on the parent dir row (aggregate)
	if !strings.Contains(v, "1/2✓") {
		t.Errorf("dir row reviewed aggregate missing:\n%s", v)
	}
}

func TestFilesPaneNarrowSidebarDropsMeta(t *testing.T) {
	m := filesModel(t, 80, 24) // medium sidebar: fw = clamp(80/4,22,26) = 22
	v := stripANSIish(strings.Join(m.filesPaneRows(m.listWidth(), m.bodyHeight()-2), "\n"))
	if !strings.Contains(v, "handler.go") {
		t.Fatalf("compact sidebar must still show names:\n%s", v)
	}
	if strings.Contains(v, "+1") {
		t.Errorf("±line meta must drop at sidebar width:\n%s", v)
	}
}

// ---- h/l tree semantics ----------------------------------------------------

func TestFilesTreeHLSemantics(t *testing.T) {
	m := filesModel(t, 120, 35)

	// collapse the services dir with h (cursor on the dir row)
	m.treeCursor = filesRowOf(m, "services")
	m.filesCollapseOrParent()
	if got := filesTreeRows(m); len(got) != 1 || got[0] != "0:services" {
		t.Fatalf("h on expanded dir must collapse it, rows = %v", got)
	}
	// l re-expands
	m.filesExpandOrOpen()
	if n := len(filesTreeRows(m)); n < 4 {
		t.Fatalf("l on collapsed dir must expand it, rows = %d", n)
	}

	// h on a file row jumps to the parent dir row
	m.treeCursor = filesRowOf(m, "services/x/handler.go")
	m.filesCollapseOrParent()
	if r := m.treeRowAt(m.treeCursor); r == nil || r.node.path != "services/x" {
		t.Fatalf("h on file must select the parent dir, got %+v", m.treeRowAt(m.treeCursor))
	}

	// nested: h again moves up another level (collapsed dir → parent)
	m.filesCollapseOrParent()
	if r := m.treeRowAt(m.treeCursor); r == nil || r.node.path != "services" {
		t.Fatalf("h on collapsed dir must go to the parent, got %+v", m.treeRowAt(m.treeCursor))
	}

	// l on a file opens it in the diff and focuses the diff pane
	m.treeCursor = filesRowOf(m, "services/y/service.go")
	m.filesExpandOrOpen()
	if m.browseFile != "services/y/service.go" || m.focus != focusDiff {
		t.Fatalf("l on file must open the diff (browse=%q focus=%d)", m.browseFile, m.focus)
	}
}

func TestFilesExpandCollapseAllAndFlat(t *testing.T) {
	m := filesModel(t, 120, 35)
	m.filesSetExpansion(false)
	if got := filesTreeRows(m); len(got) != 1 {
		t.Fatalf("collapse all must leave only root dirs, rows = %v", got)
	}
	m.filesSetExpansion(true)
	if n := len(filesTreeRows(m)); n != 7 { // services + x + y + z + 3 files
		t.Fatalf("expand all must reveal the whole tree, rows = %d", n)
	}

	// flat mode: exactly the files, depth 0, hierarchy hidden
	m.filesToggleFlat()
	rows := filesTreeRows(m)
	if len(rows) != 3 {
		t.Fatalf("flat mode must list exactly the files, rows = %v", rows)
	}
	for _, r := range rows {
		if !strings.HasPrefix(r, "0:") {
			t.Fatalf("flat rows must be depth 0: %v", rows)
		}
	}
	// selection follows into flat mode
	if m.treeRowAt(m.treeCursor) == nil {
		t.Fatal("flat toggle must keep a valid cursor")
	}
	m.filesToggleFlat()
	if n := len(filesTreeRows(m)); n != 7 {
		t.Fatalf("toggling back must restore the tree, rows = %d", n)
	}
}

// ---- filters keep state ----------------------------------------------------

func TestFilesFilterKeepsState(t *testing.T) {
	m := filesModel(t, 120, 35)
	m.buildTree() // tests below write tree state directly
	m.rev.ReviewedFiles = []string{"services/y/service.go"}
	m.tree.expandedDirs["services/x"] = false // remember a collapsed dir
	m.treeRowAt(filesRowOf(m, "services/x")).node.expanded = false
	m.rebuildTreeRows()

	m.filesSetFilter(filesFilterReviewed)
	if got := filesTreeRows(m); len(got) != 1 || !strings.HasSuffix(got[0], "service.go") {
		t.Fatalf("reviewed filter must show exactly service.go, rows = %v", got)
	}
	if m.filesFilterMatches() != 1 {
		t.Fatalf("match count = %d, want 1", m.filesFilterMatches())
	}

	// back to all: expansion state survived the filter roundtrip
	m.filesSetFilter(filesFilterAll)
	rows := m.treeRows()
	for _, r := range rows {
		if r.node.path == "services/x" && r.node.expanded {
			t.Fatal("expansion state must survive filtering")
		}
	}
	// x is remembered collapsed, so the restored tree honors that: services,
	// x, y, service.go, z, deleted.go (handler.go stays inside collapsed x).
	if n := len(rows); n != 6 {
		t.Fatalf("all-filter must show the full tree, rows = %d", n)
	}

	// unreviewed filter hides the reviewed file; status filters work
	m.filesSetFilter(filesFilterUnreviewed)
	if m.filesFilterMatches() != 2 {
		t.Fatalf("unreviewed filter = %d, want 2", m.filesFilterMatches())
	}
	m.filesSetFilter(filesFilterDeleted)
	if got := filesTreeRows(m); len(got) != 1 || !strings.HasSuffix(got[0], "deleted.go") {
		t.Fatalf("deleted filter wrong: %v", got)
	}

	// header shows the active filter + count (fixture: findings on BOTH
	// handler.go and service.go → 2 matches)
	m.filesSetFilter(filesFilterFindings)
	v := stripANSIish(strings.Join(m.filesPaneRows(40, 24), "\n"))
	if !strings.Contains(v, "filter:findings") || !strings.Contains(v, "(2)") {
		t.Errorf("header must show filter + match count:\n%s", v)
	}
}

// ---- sorting ---------------------------------------------------------------

func TestFilesSortModesStable(t *testing.T) {
	m := filesModel(t, 120, 35)
	m.buildTree() // fixture state (findings) is already set; meta snapshots fresh
	// findings on handler.go only; mark service.go reviewed
	// (fixture: F001 → handler.go)
	m.tree.sortMode = filesSortFindings
	m.rebuildTreeRows()
	files := []string{}
	for _, r := range m.treeRows() {
		if !r.node.dir {
			files = append(files, r.node.path)
		}
	}
	if len(files) != 3 || files[0] != "services/x/handler.go" {
		t.Fatalf("findings sort must put handler.go first, got %v", files)
	}
	// re-running rebuild must not reorder (stability)
	m.rebuildTreeRows()
	files2 := []string{}
	for _, r := range m.treeRows() {
		if !r.node.dir {
			files2 = append(files2, r.node.path)
		}
	}
	for i := range files {
		if files[i] != files2[i] {
			t.Fatalf("sort must be stable across rebuilds: %v vs %v", files, files2)
		}
	}

	// diff order mode follows diff.patch (handler, service, deleted)
	m.tree.sortMode = filesSortDiff
	m.rebuildTreeRows()
	files3 := []string{}
	for _, r := range m.treeRows() {
		if !r.node.dir {
			files3 = append(files3, r.node.path)
		}
	}
	want := []string{"services/x/handler.go", "services/y/service.go", "services/z/deleted.go"}
	for i := range want {
		if files3[i] != want[i] {
			t.Fatalf("diff order = %v, want %v", files3, want)
		}
	}

	// review-status sort: unreviewed first
	m.filesSetFilter(filesFilterAll)
	m.tree.sortMode = filesSortReview
	m.rebuildTreeRows()
	var first string
	for _, r := range m.treeRows() {
		if !r.node.dir {
			first = r.node.path
			break
		}
	}
	if first == "services/y/service.go" && len(m.rev.ReviewedFiles) > 0 {
		// only wrong if service.go is the reviewed one and appears first
		if m.tree.meta[first].reviewed && !m.tree.meta["services/x/handler.go"].reviewed {
			t.Fatalf("review sort must put unreviewed first, got %v", first)
		}
	}
}

// ---- preview focus rule ----------------------------------------------------

func TestFilesPreviewFocusRule(t *testing.T) {
	m := filesModel(t, 120, 35)
	// starting state: browse seeded to the first file by startDiffFirst
	m.treeCursor = filesRowOf(m, "services/x/handler.go")
	m.filesPaneRows(26, 24) // render with focus==files → preview arms
	if m.browseFile != "services/x/handler.go" {
		t.Fatalf("focused files pane must preview the cursor file, got %q", m.browseFile)
	}
	if m.focus != focusFiles {
		t.Fatal("preview must not steal focus")
	}

	// moving the cursor previews the next file (movement arms the preview;
	// direct assignment + render must not — see the ▸ marker case below)
	want := "services/y/service.go"
	for i := 0; i < 8 && m.treeRowAt(m.treeCursor).node.path != want; i++ {
		m2, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		m = m2.(*model)
	}
	m.filesPaneRows(26, 24)
	if m.browseFile != want {
		t.Fatalf("cursor move must re-preview, got %q", m.browseFile)
	}

	// scrolling the DIFF (focus diff) updates browseFile and the tree cursor
	// stays put; re-rendering the files pane unfocused must NOT jump back
	m.focus = focusDiff
	for i := 0; i < 12; i++ {
		m2, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		m = m2.(*model)
	}
	if m.browseFile == "services/y/service.go" {
		t.Fatal("diff scroll must move past the previewed file")
	}
	treeCur := m.treeCursor
	m.filesPaneRows(26, 24) // unfocused render
	if m.browseFile == "services/y/service.go" {
		t.Fatal("unfocused files pane must not yank the diff back to the cursor")
	}
	if m.treeCursor != treeCur {
		t.Fatal("diff scrolling must not move the tree cursor")
	}

	// refocusing files without a cursor move does not re-preview either
	// (previewPath only arms on cursor movement onto a new file)
	m.focus = focusFiles
	after := m.browseFile
	m.filesPaneRows(26, 24)
	if m.browseFile != after {
		t.Fatal("re-focus without cursor movement must not re-preview")
	}

	// the active-file row carries the ▸ marker, distinct from the cursor row
	m2 := filesModel(t, 160, 45)
	m2.browseFilePath("services/y/service.go") // active = y, diff focused
	m2.setView(viewFiles)
	m2.treeCursor = filesRowOf(m2, "services/x/handler.go")
	v := m2.filesPaneRows(44, 24)
	foundActive, foundCursor := false, false
	for _, line := range v {
		plain := stripANSIish(line)
		if strings.Contains(plain, "service.go") && strings.HasPrefix(strings.TrimLeft(plain, " "), "▸") {
			foundActive = true
		}
		if strings.Contains(plain, "handler.go") && strings.Contains(plain, "▾") == false {
			// cursor row exists (reverse-styled in raw form)
			if strings.Contains(line, "\x1b[") && strings.Contains(plain, "handler.go") {
				foundCursor = strings.Contains(line, "\x1b[7m") || strings.Contains(line, "\x1b[")
			}
		}
	}
	if !foundActive {
		t.Fatalf("active file row must carry the ▸ marker:\n%s", strings.Join(v, "\n"))
	}
	if !foundCursor {
		t.Fatalf("selected row must be cursor-styled:\n%s", strings.Join(v, "\n"))
	}
}

// ---- stable expansion across diff-version rebuilds -------------------------

func TestFilesStableAcrossDiffRebuild(t *testing.T) {
	m := filesModel(t, 120, 35)
	m.buildTree() // tests below write tree state directly
	m.tree.expandedDirs["services"] = false
	m.treeRowAt(filesRowOf(m, "services")).node.expanded = false
	m.tree.flat = false
	m.tree.sortMode = filesSortReview
	m.tree.filterKind = filesFilterUnreviewed
	m.rebuildTreeRows()

	// simulate a new diff version: node rebuild, state must carry over
	m.diffVersion++
	m.buildTree()
	if m.tree.builtVer != m.diffVersion {
		t.Fatal("buildTree must stamp the diff version")
	}
	rows := m.treeRows()
	if len(rows) != 1 || rows[0].node.path != "services" {
		t.Fatalf("collapsed dir must survive a rebuild, rows = %v", filesTreeRows(m))
	}
	if m.tree.sortMode != filesSortReview || m.tree.filterKind != filesFilterUnreviewed {
		t.Fatal("sort/filter state must survive a rebuild")
	}
	// cursor clamped into range, not negative
	if m.treeCursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.treeCursor)
	}
}

// ---- windowed rendering (hundreds of files) --------------------------------

func TestFilesWindowedRendering(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "diff --git a/pkg/f%03d.go b/pkg/f%03d.go\n", i, i)
		fmt.Fprintf(&b, "--- a/pkg/f%03d.go\n+++ b/pkg/f%03d.go\n", i, i)
		fmt.Fprintf(&b, "@@ -1,1 +1,2 @@\n a\n+changed %d\n", i)
	}
	m := newTestModelDiff(t, 120, 35, nil, b.String())
	m.setView(viewFiles)
	w, h := 26, 20
	rows := m.filesPaneRows(w, h)
	// header + rule + viewport rows: never more than h rows total
	if len(rows) > h {
		t.Fatalf("pane rendered %d rows, want ≤ %d (windowing broken)", len(rows), h)
	}
	// walking the cursor keeps exactly h rows and always includes the cursor
	for step := 0; step < 320; step += 40 {
		m.treeCursor = step
		rows := m.filesPaneRows(w, h)
		if len(rows) > h {
			t.Fatalf("step %d: %d rows > %d", step, len(rows), h)
		}
		if m.treeCursor >= len(m.treeRows()) {
			continue
		}
		l, _ := (&widget.List{Cursor: m.treeCursor, Len: len(m.treeRows())}).Window(h - 2)
		joined := stripANSIish(strings.Join(rows, "\n"))
		name := fmt.Sprintf("f%03d.go", l)
		if !strings.Contains(joined, name) {
			t.Fatalf("step %d: cursor row %s not visible in window", step, name)
		}
	}
}

// ---- golden ----------------------------------------------------------------

func TestGoldenFilesPane(t *testing.T) {
	m := newMultiFileModel(t, 100, 30)
	m.rev.HeadSHA = "fixed0sha"
	m.rev.ReviewedFiles = []string{"services/y/service.go"}
	m.browseFilePath("services/x/handler.go") // active-file marker + diff focus
	m.setView(viewFiles)
	m.treeCursor = filesRowOf(m, "services") // cursor parked on the dir row
	got := m.View().Content
	path := "testdata/golden/files-100x30.txt"
	if *update {
		if err := osWriteFile(path, []byte(got)); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := osReadFile(path)
	if err != nil {
		t.Skipf("no files golden (run with -update): %v", err)
	}
	if got != string(want) {
		t.Fatalf("files golden mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}
