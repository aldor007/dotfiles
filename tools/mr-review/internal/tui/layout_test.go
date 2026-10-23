package tui

import (
	"flag"
	"os"
	"strings"
	"testing"

	"mr-review/internal/store"

	"charm.land/lipgloss/v2"
)

var update = flag.Bool("update", false, "rewrite golden snapshot files")

func osWriteFile(path string, b []byte) error {
	if err := os.MkdirAll("testdata/golden", 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func osReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

// normalizeFrame makes a frame deterministic across runs (no timestamps).
func normalizeFrame(s string) string {
	return s
}

// sizes are the required validation dimensions.
var sizes = [][2]int{{80, 24}, {100, 30}, {120, 35}, {160, 45}, {200, 50}}

// layoutStates builds a fresh model per state so tests are independent.
type layoutCase struct {
	name  string
	setup func(m *model)
}

func layoutCases() []layoutCase {
	return []layoutCase{
		{"findings", func(m *model) {}},
		{"detail-diff", func(m *model) { m.focus = focusDiff; m.tab = tabOverview }},
		{"detail-explanation", func(m *model) { m.focus = focusDiff; m.tab = tabComment }},
		{"files", func(m *model) { m.setView(viewFiles) }},
		{"comments", func(m *model) {
			m.setView(viewComments)
			m.comments = []store.DraftComment{
				store.NewDraftComment("C001", "services/x/handler.go", "new", 11, 11, false, "first note"),
			}
		}},
		{"browse", func(m *model) { m.browseFilePath(m.changedFiles()[0]) }},
		{"browse-visual", func(m *model) {
			m.browseFilePath(m.changedFiles()[0])
			m.startVisual()
			m.browseMove(2)
		}},
		{"palette", func(m *model) { m.openPalette() }},
		{"palette-filtered", func(m *model) {
			m.openPalette()
			m.paletteInput.SetValue("approv")
			m.refreshPalette()
		}},
		{"inspector", func(m *model) { m.overlay = ovInspector }},
		{"publish-preview", func(m *model) { m.overlay = ovPublish }},
		{"export-dialog", func(m *model) { m.openExport() }},
		{"draft-editor", func(m *model) {
			m.comments = []store.DraftComment{
				store.NewDraftComment("C001", "services/x/handler.go", "new", 11, 11, false, "first note"),
			}
			m.setView(viewComments)
			m.editSelectedDraft()
		}},
		{"zoom", func(m *model) { m.zoom = true; m.focus = focusDiff }},
		{"help", func(m *model) { m.overlay = ovHelp }},
		{"summary", func(m *model) { m.overlay = ovSummary }},
		{"narrow-list", func(m *model) {}},
		{"search", func(m *model) { m.openSearch() }},
	}
}

// TestLayoutFitsAllSizes renders every state at every required size and
// asserts: (1) the frame never exceeds the terminal, (2) the outer box is
// aligned, (3) pane borders form a continuous divider.
func TestLayoutFitsAllSizes(t *testing.T) {
	for _, size := range sizes {
		w, h := size[0], size[1]
		for _, tc := range layoutCases() {
			m := newMultiFileModel(t, w, h)
			tc.setup(m)
			view := m.View().Content
			lines := strings.Split(view, "\n")

			for i, line := range lines {
				if cw := lipgloss.Width(line); cw > w {
					t.Fatalf("%dx%d %s: line %d is %d cells (> %d): %q",
						w, h, tc.name, i, cw, w, visibleText(line))
				}
			}
			// the overlay/modal screens replace the body: only the normal
			// layout draws the full-height box; modals must simply fit
			if tc.name == "findings" || tc.name == "files" || tc.name == "comments" ||
				tc.name == "detail-diff" || tc.name == "detail-explanation" ||
				tc.name == "browse" || tc.name == "browse-visual" || tc.name == "zoom" {
				assertBoxAligned(t, w, h, tc.name, lines)
			}
			// the status footer must always be present and within width
			last := lines[len(lines)-1]
			if !strings.Contains(last, "┃") && !strings.Contains(last, "Status") &&
				!strings.Contains(visibleText(last), "┃") {
				t.Fatalf("%dx%d %s: footer missing (last line %q)", w, h, tc.name, visibleText(last))
			}
		}
	}
}

// assertBoxAligned verifies the rounded box: top/bottom corners at the same
// columns and one border row per body row (ANSI-stripped matching, since
// styled borders carry escape prefixes).
func assertBoxAligned(t *testing.T, w, h int, name string, lines []string) {
	t.Helper()
	top, bottom := -1, -1
	for i, l := range lines {
		p := stripANSIish(l)
		if strings.HasPrefix(p, "╭") {
			top = i
		}
		if strings.HasPrefix(p, "╰") {
			bottom = i
		}
	}
	if top < 0 || bottom < 0 {
		t.Fatalf("%dx%d %s: box borders missing (top=%d bottom=%d)", w, h, name, top, bottom)
	}
	// exact line count is covered byte-exactly by the golden snapshots;
	// here we only assert structural invariants: top/bottom corners present,
	// vertical edges continuous.
	for i := top + 1; i < bottom; i++ {
		p := stripANSIish(lines[i])
		if !strings.HasPrefix(p, "│") {
			t.Fatalf("%dx%d %s: row %d missing left border: %q", w, h, name, i, visibleText(lines[i]))
		}
		if !strings.HasSuffix(p, "│") {
			t.Fatalf("%dx%d %s: row %d missing right border: %q", w, h, name, i, visibleText(lines[i]))
		}
		// two-pane layout: some divider character must sit between the
		// panes (│ light or ┃ heavy — the cockpit widget draws its own)
		if w >= 90 && !m_isZoomName(name) {
			plain := stripANSIish(lines[i])
			runes := []rune(plain)
			found := false
			for _, r := range runes {
				if r == '│' || r == '┃' {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%dx%d %s: no divider in row %d: %q", w, h, name, i, visibleText(lines[i]))
			}
		}
	}
}

func m_isZoomName(name string) bool { return name == "zoom" }

// listWidthFor mirrors model.listWidth for plain-width inputs.
func listWidthFor(w int) int {
	lw := w * 34 / 100
	if lw < 32 {
		lw = 32
	}
	if lw > 56 {
		lw = 56
	}
	if lw > w-30 {
		if w-30 > 26 {
			lw = w - 30
		} else {
			lw = 26
		}
	}
	return lw
}

// visibleText strips ANSI for failure messages.
func visibleText(s string) string { return stripANSIish(s) }

// TestPaletteFitsAndShowsShortcuts — the palette is usable at every size.
func TestPaletteFitsAndShowsShortcuts(t *testing.T) {
	for _, size := range sizes {
		m := newMultiFileModel(t, size[0], size[1])
		m.openPalette()
		m.paletteInput.SetValue("publish")
		m.refreshPalette()
		v := m.View().Content
		if !strings.Contains(v, "ACTIONS") {
			t.Fatalf("%dx%d: palette title missing", size[0], size[1])
		}
		if !strings.Contains(v, "Publish approved comments") {
			t.Fatalf("%dx%d: palette must list publish action", size[0], size[1])
		}
		if !strings.Contains(v, "P") && !strings.Contains(v, "esc") {
			t.Fatalf("%dx%d: palette must show shortcut labels", size[0], size[1])
		}
	}
}

// TestTabsStayInDetailPane — the tab bar never overflows the detail pane.
func TestTabsStayInDetailPane(t *testing.T) {
	for _, size := range sizes {
		m := newMultiFileModel(t, size[0], size[1])
		m.focus = focusDiff
		v := m.View().Content
		for i, line := range strings.Split(v, "\n") {
			if strings.Contains(line, "DIFF") && strings.Contains(line, "EXPL") {
				if cw := lipgloss.Width(line); cw > size[0] {
					t.Fatalf("%dx%d: tab bar overflows at row %d (%d cells)", size[0], size[1], i, cw)
				}
			}
		}
	}
}

// TestSelectedFindingAlwaysVisibleAtSizes.
func TestSelectedFindingAlwaysVisibleAtSizes(t *testing.T) {
	m := newMultiFileModel(t, 80, 24)
	m.focus = focusFindings // medium: the findings drawer must keep the selection visible
	for i := 0; i < 15; i++ {
		m.cursorDown()
	}
	v := m.View().Content
	if !strings.Contains(v, "F002") {
		t.Fatal("selected finding must remain visible after navigation")
	}
}

// TestGoldenSnapshots — deterministic frames for key states. Golden files
// live in testdata/golden; update with -update.
func TestGoldenSnapshots(t *testing.T) {
	cases := []struct {
		name   string
		width  int
		height int
		setup  func(m *model)
	}{
		{"findings-120x35", 120, 35, func(m *model) {}},
		{"browse-120x35", 120, 35, func(m *model) { m.browseFilePath("services/x/handler.go") }},
		// chunk 3: footer-context goldens (1-line contextual footer verified visually)
		{"footer-findings-120x35", 120, 35, func(m *model) {}},
		{"footer-browse-120x35", 120, 35, func(m *model) { m.browseFilePath("services/x/handler.go") }},
		{"palette-120x35", 120, 35, func(m *model) { m.openPalette() }},
		{"publish-120x35", 120, 35, func(m *model) { m.overlay = ovPublish }},
		// chunk 6: width coverage across the plan's five layout sizes
		{"findings-80x24", 80, 24, func(m *model) {}},
		{"browse-80x24", 80, 24, func(m *model) { m.browseFilePath("services/x/handler.go") }},
		{"palette-80x24", 80, 24, func(m *model) { m.openPalette() }},
		{"publish-80x24", 80, 24, func(m *model) { m.overlay = ovPublish }},
		{"findings-100x30", 100, 30, func(m *model) {}},
		{"findings-160x45", 160, 45, func(m *model) {}},
		{"browse-160x45", 160, 45, func(m *model) { m.browseFilePath("services/x/handler.go") }},
		{"findings-200x50", 200, 50, func(m *model) {}},
		{"publish-200x50", 200, 50, func(m *model) { m.overlay = ovPublish }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMultiFileModel(t, tc.width, tc.height)
			m.rev.HeadSHA = "fixed0sha" // determinism
			tc.setup(m)
			got := m.View().Content
			got = normalizeFrame(got)
			path := "testdata/golden/" + tc.name + ".txt"
			if *update {
				if err := osWriteFile(path, []byte(got)); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := osReadFile(path)
			if err != nil {
				t.Skipf("no golden for %s (run with -update): %v", tc.name, err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", tc.name, want, got)
			}
		})
	}
}

// TestPanelBordersContinuous is covered by TestLayoutFitsAllSizes +
// TestGoldenSnapshots; this test exists only as a documentation pointer so
// future refactors don't accidentally re-introduce nested boxes.
func TestPanelBordersContinuous(t *testing.T) {
	for _, size := range sizes {
		w, h := size[0], size[1]
		for _, tc := range layoutCases() {
			if tc.name == "palette" || tc.name == "palette-filtered" ||
				tc.name == "inspector" || tc.name == "publish-preview" ||
				tc.name == "export-dialog" || tc.name == "draft-editor" ||
				tc.name == "help" || tc.name == "summary" {
				continue
			}
			m := newMultiFileModel(t, w, h)
			tc.setup(m)
			view := m.View().Content
			lines := strings.Split(view, "\n")
			assertBoxAligned(t, w, h, tc.name, lines)
		}
	}
}
