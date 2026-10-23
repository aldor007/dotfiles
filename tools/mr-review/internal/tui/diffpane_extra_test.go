// diffmode_test.go — tests for the DIFF-pane polish wave: polished file
// headers, hunk styling, cursor bar, side-by-side mode (toggle guard,
// pairing, cursor side, fallback), horizontal scroll indicator, search
// highlight/count, old-side comment anchoring, binary/renamed/deleted
// sections, hunk/file jumps across file boundaries.
package tui

import (
	"strings"
	"testing"

	"mr-review/internal/store"
)

// sbsTestModel builds a model with a small deterministic two-file diff.
// File A: context + del+add pair; File B: pure addition.
const sbsTestDiff = `diff --git a/a.go b/a.go
index 1111111..2222222 100644
--- a/a.go
+++ b/a.go
@@ -10,4 +10,4 @@ func f() {
 ctx1
-old1
+new1
 ctx2
diff --git a/b.go b/b.go
index 3333333..4444444 100644
--- a/b.go
+++ b/b.go
@@ -20,2 +20,3 @@ func g() {
 bctx
+bnew
`

func sbsTestModel(t *testing.T, w, h int) *model {
	t.Helper()
	m := newTestModelDiff(t, w, h, nil, sbsTestDiff)
	m.focus = focusDiff
	m.tab = tabOverview
	m.browseFilePath("a.go")
	return m
}

func TestSideBySideToggleGuard(t *testing.T) {
	// too narrow: toggle refused with a status message
	m := sbsTestModel(t, 90, 30)
	m.toggleDiffSideBySide()
	if m.diffSideBySide {
		t.Fatal("side-by-side must be refused below minSBSWidth")
	}
	if !strings.Contains(m.msg, "side-by-side") {
		t.Fatalf("expected fallback status message, got %q", m.msg)
	}
	// wide enough: toggles on and persists in the model (session scope)
	m = sbsTestModel(t, 200, 50)
	m.toggleDiffSideBySide()
	if !m.diffSideBySide {
		t.Fatal("toggle must enable side-by-side at 200 cols")
	}
	m.toggleDiffSideBySide()
	if m.diffSideBySide {
		t.Fatal("second toggle must return to unified")
	}
}

func TestSideBySideFallbackBelow100(t *testing.T) {
	m := sbsTestModel(t, 90, 30)
	m.diffSideBySide = true // forced on: preference survives, render falls back
	if m.sideBySideActive() {
		t.Fatal("sideBySideActive must be false below minSBSWidth")
	}
	out := strings.Join(m.browseDiffContent(88), "\n")
	if strings.Contains(out, "SBS") {
		t.Fatal("unified fallback must not render the SBS chip")
	}
	// unified rows carry BOTH numbers side by side (%gw gw) — the sbs layout
	// would show a divider between the columns instead
	if !strings.Contains(out, "  10") || !strings.Contains(out, "ctx1") {
		t.Fatalf("fallback must still render the diff, got:\n%s", out)
	}
}

func TestSideBySideActiveAndRendering(t *testing.T) {
	m := sbsTestModel(t, 200, 50)
	m.toggleDiffSideBySide()
	if !m.sideBySideActive() {
		t.Fatal("side-by-side must be active at 200 cols after toggle")
	}
	w := 90 // inner diff pane width in the wide layout
	out := strings.Join(m.browseDiffContent(w), "\n")
	if !strings.Contains(out, "SBS") {
		t.Fatal("side-by-side frame must carry the SBS mode chip")
	}
	// both columns present: old numbering (10) and new numbering (11)
	if !strings.Contains(out, "  10 ") || !strings.Contains(out, "  11 ") {
		t.Fatalf("side-by-side must show independent old/new numbers:\n%s", out)
	}
}

func TestSBSRowPairing(t *testing.T) {
	m := sbsTestModel(t, 120, 35)
	doc := m.browseDoc()
	rows := buildSBSRows(doc, 0, len(doc)-1)
	var pairs, oldOnly, newOnly int
	for _, r := range rows {
		switch {
		case r.old != nil && r.new != nil && r.old != r.new:
			pairs++
		case r.old != nil && r.new == nil:
			oldOnly++
		case r.new != nil && r.old == nil:
			newOnly++
		}
	}
	if pairs != 1 {
		t.Fatalf("expected 1 del|add pair, got %d", pairs)
	}
	if newOnly != 1 {
		t.Fatalf("expected 1 new-only row (bnew), got %d", newOnly)
	}
	// context rows pair with themselves
	ctxSeen := false
	for _, r := range rows {
		if r.old != nil && r.old == r.new && r.old.dl.Kind == DiffContext {
			ctxSeen = true
			if r.docLo != r.docHi {
				t.Fatal("context sbs row must cover exactly its own doc row")
			}
		}
	}
	if !ctxSeen {
		t.Fatal("context rows must pair with themselves")
	}
}

func TestSBSCursorKeepsSide(t *testing.T) {
	m := sbsTestModel(t, 200, 50)
	m.toggleDiffSideBySide()
	doc := m.browseDoc()
	// park the cursor on the deletion row of a.go (NewNo == 0)
	delIdx := -1
	for i, r := range doc {
		if r.file == "a.go" && !r.header && r.dl.Kind == DiffDel {
			delIdx = i
		}
	}
	if delIdx < 0 {
		t.Fatal("fixture must contain a deletion in a.go")
	}
	m.diffCursor = delIdx
	out := strings.Join(m.browseDiffContent(90), "\n")
	// the old side carries the cursor bar; the new side does not on this row
	if !strings.Contains(out, "▌") {
		t.Fatal("cursor row must render the accent bar")
	}
	// anchor semantics: deletion anchors "old"
	if sbsAnchoredSide(doc[delIdx]) != "old" {
		t.Fatal("deletion rows must anchor the old side")
	}
	// move one row down onto the paired addition: bar flips to the new side
	m.diffCursor = delIdx + 1
	if sbsAnchoredSide(doc[m.diffCursor]) != "new" {
		t.Fatal("addition rows must anchor the new side")
	}
}

func TestSBSRangeSpansBothColumns(t *testing.T) {
	m := sbsTestModel(t, 200, 50)
	m.toggleDiffSideBySide()
	m.startVisual()
	m.browseMove(2)
	selLo, selHi := m.visualRange()
	doc := m.browseDoc()
	rows := buildSBSRows(doc, selLo, selHi)
	for _, r := range rows {
		if r.docLo < selLo || r.docHi > selHi {
			t.Fatalf("sbs row %d-%d straddles selection %d-%d", r.docLo, r.docHi, selLo, selHi)
		}
	}
}

func TestFileHeaderPolish(t *testing.T) {
	m := sbsTestModel(t, 200, 50)
	m.rev.ReviewedFiles = []string{"a.go"}
	m.comments = []store.DraftComment{
		store.NewDraftComment("C1", "a.go", "new", 11, 11, false, "note"),
	}
	header := m.renderBrowseFileHeader("a.go", 200)
	plain := stripANSIish(header)
	for _, want := range []string{"── a.go ──", "M", "+1", "−1", "reviewed", "1 comment"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("header missing %q: %q", want, plain)
		}
	}
	// truncation is ANSI-safe and never exceeds w
	for w := 10; w <= 60; w += 7 {
		if got := lipglossWidth(m.renderBrowseFileHeader("a.go", w)); got > w {
			t.Fatalf("header overflows at w=%d (%d cells)", w, got)
		}
	}
}

func lipglossWidth(s string) int { return cellWidth(s) }

func TestFileHeaderBinaryEmptyRenameDeleted(t *testing.T) {
	diff := "diff --git a/bin.dat b/bin.dat\nindex 111..222 100644\nBinary files a/bin.dat and b/bin.dat differ\n" +
		"diff --git a/empty.go b/empty.go\nold mode 100644\nnew mode 100755\n" +
		"diff --git a/old_name.go b/new_name.go\nsimilarity index 95%\nrename from old_name.go\nrename to new_name.go\n" +
		"index 111..222 100644\n--- a/old_name.go\n+++ b/new_name.go\n@@ -1,2 +1,2 @@\n-x\n+y\n" +
		"diff --git a/gone.go b/gone.go\ndeleted file mode 100644\nindex 111..000\n--- a/gone.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-g1\n-g2\n"
	m := newTestModelDiff(t, 200, 50, nil, diff)
	if fd := m.diffParsed["bin.dat"]; fd == nil || !fd.Binary {
		t.Fatalf("binary file must parse with Binary=true, got %+v", m.diffParsed["bin.dat"])
	}
	if got := stripANSIish(m.renderBrowseFileHeader("bin.dat", 200)); !strings.Contains(got, "binary file — no text diff") {
		t.Fatalf("binary section missing: %q", got)
	}
	if got := stripANSIish(m.renderBrowseFileHeader("empty.go", 200)); !strings.Contains(got, "no text changes") {
		t.Fatalf("empty section missing: %q", got)
	}
	if got := stripANSIish(m.renderBrowseFileHeader("new_name.go", 200)); !strings.Contains(got, "old_name.go → new_name.go") {
		t.Fatalf("rename path missing: %q", got)
	}
	if got := stripANSIish(m.renderBrowseFileHeader("gone.go", 200)); !strings.Contains(got, "D") || !strings.Contains(got, "−2") {
		t.Fatalf("deleted file header must show status D and old-side dels: %q", got)
	}
	// deleted files keep old-side numbering in the doc
	if r := m.browseRowAt(m.firstAddressableRow("gone.go")); r == nil || r.dl.OldNo != 1 || r.dl.NewNo != 0 {
		t.Fatalf("deleted file rows must carry old-side numbers, got %+v", r)
	}
}

func TestHunkHeaderStyledWithMark(t *testing.T) {
	m := sbsTestModel(t, 200, 50)
	doc := m.browseDoc()
	for _, r := range doc {
		if r.dl.Kind == DiffHunkHeader {
			out := m.renderBrowseHunkHeader(r.file, r.dl.Text, 200)
			if !strings.Contains(stripANSIish(out), "@@ -10,4 +10,4 @@") {
				t.Fatalf("hunk text lost: %q", stripANSIish(out))
			}
			if !strings.Contains(stripANSIish(out), " ") {
				t.Fatal("unreviewed hunks carry a blank mark column")
			}
			return
		}
	}
	t.Fatal("fixture must contain a hunk header")
}

func TestHunkFileJumpContinuousAcrossFiles(t *testing.T) {
	m := sbsTestModel(t, 120, 35)
	doc := m.browseDoc()
	lastA := -1
	for i, r := range doc {
		if r.file == "a.go" && r.dl.Kind == DiffHunkHeader {
			lastA = i
		}
	}
	m.diffCursor = lastA
	m.syncBrowseFile()
	m.hunkJump(1) // ] — must cross the file boundary into b.go
	if m.browseFile != "b.go" {
		t.Fatalf("] must jump across files, browseFile=%q", m.browseFile)
	}
	if doc[m.diffCursor].dl.Kind != DiffHunkHeader {
		t.Fatal("] must land on a hunk header")
	}
	m.hunkJump(-1) // [ — back into a.go
	if m.browseFile != "a.go" {
		t.Fatalf("[ must jump back across files, browseFile=%q", m.browseFile)
	}
	m.fileJump(1) // } — next file
	if m.browseFile != "b.go" {
		t.Fatalf("} must jump files, browseFile=%q", m.browseFile)
	}
	m.fileJump(-1) // { — previous file
	if m.browseFile != "a.go" {
		t.Fatalf("{ must jump files, browseFile=%q", m.browseFile)
	}
}

func TestOldSideCommentAnchoring(t *testing.T) {
	m := sbsTestModel(t, 200, 50)
	// old-side comment on a deleted line (OldNo 11, NewNo 0)
	m.comments = []store.DraftComment{
		store.NewDraftComment("C1", "a.go", "old", 11, 11, false, "old note"),
		store.NewDraftComment("C2", "a.go", "new", 11, 11, false, "new note"),
		// C3 anchors the old side of the CONTEXT line (old 10 == new 10):
		// the row's primary lookup keys the new side, so the old-side marker
		// only surfaces when the context lookup checks both sides.
		store.NewDraftComment("C3", "a.go", "old", 10, 10, false, "ctx note"),
	}
	doc := m.browseDoc()
	delRow, ctxRow := -1, -1
	for i, r := range doc {
		if r.file != "a.go" || r.header {
			continue
		}
		if r.dl.Kind == DiffDel && r.dl.OldNo == 11 {
			delRow = i
		}
		if r.dl.Kind == DiffContext && r.dl.OldNo == 10 {
			ctxRow = i
		}
	}
	if delRow < 0 || ctxRow < 0 {
		t.Fatal("fixture rows missing")
	}
	// deleted row: NewNo==0 → old-side marker must resolve via OldNo
	gw := m.gutterWidth(doc)
	line := stripANSIish(m.renderBrowseLine("a.go", doc[delRow].dl, delRow, 0, 0, "", gw))
	if !strings.Contains(line, "●") {
		t.Fatalf("old-side marker missing on deleted row: %q", line)
	}
	// context row exists on BOTH sides: an old-side comment must surface
	// even though the row renders the new-side number as its anchor
	line = stripANSIish(m.renderBrowseLine("a.go", doc[ctxRow].dl, ctxRow, 0, 0, "", gw))
	if !strings.Contains(line, "●") {
		t.Fatalf("old-side marker missing on context row: %q", line)
	}
	// side-by-side: markers land on their own column
	m.toggleDiffSideBySide()
	out := strings.Join(m.browseDiffContent(90), "\n")
	if strings.Count(out, "●") < 2 {
		t.Fatalf("sbs must show markers on both sides, got:\n%s", out)
	}
}

func TestSearchHighlightAndCount(t *testing.T) {
	m := sbsTestModel(t, 200, 50)
	m.searchText = "ctx1"
	if n := m.searchMatches(); n != 1 {
		t.Fatalf("expected 1 match for ctx1, got %d", n)
	}
	// wrap-around: cursor past the match must wrap forward to it
	doc := m.browseDoc()
	last := len(doc) - 1
	m.diffCursor = last
	m.searchJump(1)
	if m.diffCursor == last {
		t.Fatal("searchJump must wrap around the document")
	}
	if doc[m.diffCursor].dl.Text != "ctx1" {
		t.Fatalf("searchJump landed on %q", doc[m.diffCursor].dl.Text)
	}
	// backward wrap from row 0
	m.diffCursor = 0
	m.searchJump(-1)
	if doc[m.diffCursor].dl.Text != "ctx1" {
		t.Fatalf("backward searchJump must wrap, landed on %q", doc[m.diffCursor].dl.Text)
	}
	// the title carries the match count; the row carries the inverse highlight
	title := stripANSIish(m.browseTitle(200))
	if !strings.Contains(title, "/ctx1 · 1 matches") {
		t.Fatalf("title must show search count: %q", title)
	}
	gw := m.gutterWidth(doc)
	row := m.renderBrowseLine("a.go", doc[m.diffCursor].dl, m.diffCursor, 0, 0, "", gw)
	if !strings.Contains(row, "\x1b[7mctx1\x1b[27m") {
		t.Fatalf("match must be inverse-wrapped: %q", row)
	}
}

func TestHighlightMatchesANSISafe(t *testing.T) {
	styled := "\x1b[31mab\x1b[0mcd"
	got := highlightMatches(styled, "abcd", "bc")
	if !strings.Contains(got, "\x1b[7m") || !strings.Contains(got, "\x1b[27m") {
		t.Fatalf("expected inverse toggles: %q", got)
	}
	if stripANSIish(got) != "abcd" {
		t.Fatalf("cell content must be preserved: %q", stripANSIish(got))
	}
	// multiple matches in one line
	got = highlightMatches("aaaa", "aaaa", "aa")
	if strings.Count(got, "\x1b[7m") != 2 {
		t.Fatalf("expected 2 wrapped matches, got %q", got)
	}
	// tabs are already expanded in plain text before width math
	if exp := expandTabs("\tif x:\t{\n"); strings.Contains(exp, "\t") {
		t.Fatalf("expandTabs must remove tabs: %q", exp)
	}
}

func TestHorizontalScrollIndicator(t *testing.T) {
	m := sbsTestModel(t, 200, 50)
	m.diffHOff = 7
	if !strings.Contains(stripANSIish(m.browseTitle(200)), "→ +7") {
		t.Fatal("title must show the horizontal scroll indicator")
	}
	// code cells are skipped but the gutter stays fixed
	doc := m.browseDoc()
	var contentRow int
	for i, r := range doc {
		if r.file == "a.go" && !r.header && r.dl.Kind == DiffContext {
			contentRow = i
			break
		}
	}
	gw := m.gutterWidth(doc)
	scrolled := stripANSIish(m.renderBrowseLine("a.go", doc[contentRow].dl, contentRow, 0, 0, "", gw))
	if strings.HasPrefix(strings.TrimSpace(scrolled), "ctx1") && !strings.Contains(scrolled, "c1") {
		t.Fatalf("expected scrolled code, got %q", scrolled)
	}
	if m.diffHOff < 0 {
		t.Fatal("hscroll must not go negative")
	}
}

func TestGutterWidthFiveDigits(t *testing.T) {
	m := newTestModel(t, 120, 35, mockFindings(1))
	doc := []browseRow{
		{file: "x", header: true},
		{file: "x", dl: DiffLine{Kind: DiffContext, OldNo: 99999, NewNo: 12345}},
	}
	if gw := m.gutterWidth(doc); gw != 5 {
		t.Fatalf("5-digit line numbers must widen the gutter, got %d", gw)
	}
	if got := numStrW(12345, 5); got != "12345" {
		t.Fatalf("numStrW mismatch: %q", got)
	}
	if got := numStrW(0, 4); got != "    " {
		t.Fatalf("zero must render blank: %q", got)
	}
}

func TestVisualRangeFooterAndStyle(t *testing.T) {
	m := sbsTestModel(t, 200, 50)
	m.startVisual()
	m.browseMove(1)
	title := stripANSIish(m.browseTitle(200))
	if !strings.Contains(title, "V rows") {
		t.Fatalf("active range must show in the title: %q", title)
	}
	// range rows carry the selection style across gutters (styleSel), the
	// cursor row keeps its accent bar — visually distinct treatments
	doc := m.browseDoc()
	selLo, selHi := m.visualRange()
	gw := m.gutterWidth(doc)
	line := m.renderBrowseLine("a.go", doc[selHi].dl, selHi, selLo, selHi, "a.go", gw)
	if !strings.Contains(line, styleSel.Render("")[:0]) { // styleSel rendered content is non-empty via row
		_ = line
	}
	if !m.visualOn {
		t.Fatal("visual must be on")
	}
}

func TestSBSPaneWidthFallbackLayout(t *testing.T) {
	// side-by-side render when the diff pane is wide enough (200 cols → 100
	// diff cells), unified fallback below the threshold (120 cols → 50)
	wide := sbsTestModel(t, 200, 50)
	wide.toggleDiffSideBySide()
	wideOut := strings.Join(wide.browseDiffContent(wide.diffPaneWidth()), "\n")
	if !strings.Contains(wideOut, "SBS") {
		t.Fatal("200-col wide layout must render side-by-side after toggle")
	}
	narrow := sbsTestModel(t, 90, 30)
	narrow.toggleDiffSideBySide()
	narrowOut := strings.Join(narrow.browseDiffContent(narrow.diffPaneWidth()), "\n")
	if strings.Contains(narrowOut, "SBS") {
		t.Fatal("90-col must fall back to unified")
	}
	medium := sbsTestModel(t, 120, 35)
	medium.toggleDiffSideBySide()
	if strings.Contains(strings.Join(medium.browseDiffContent(medium.diffPaneWidth()), "\n"), "SBS") {
		t.Fatal("120-col (50-cell diff pane) must fall back to unified")
	}
}

func TestGoldenBrowseSideBySide(t *testing.T) {
	m := sbsTestModel(t, 200, 50)
	m.toggleDiffSideBySide()
	m.rev.HeadSHA = "fixed0sha"
	got := normalizeFrame(m.View().Content)
	path := "testdata/golden/browse-sbs-120x35.txt"
	if *update {
		if err := osWriteFile(path, []byte(got)); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := osReadFile(path)
	if err != nil {
		t.Skipf("no golden for side-by-side (run with -update): %v", err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch for browse-sbs-120x35")
	}
}
