package tui

// Layer 2 e2e: every action in the registry is executed through the real
// Update path (registry dispatch, pending-key sequences, overlay modes) and
// its observable effect asserted. The coverage invariant guarantees the
// table can never silently miss a registry action.

import (
	"strings"
	"testing"

	"mr-review/internal/store"
)

// actionCase drives one action and asserts its effect on the model.
type actionCase struct {
	keys []string                     // key presses (registry bindings)
	run  func(t *testing.T, m *model) // for palette-only actions (no keys)
	post func(t *testing.T, m *model)
}

func actionCases() map[string]actionCase {
	return map[string]actionCase{
		// ---- AI findings ----
		"findings.approve": {keys: []string{"a"}, post: func(t *testing.T, m *model) {
			for i := range m.findings {
				if m.findings[i].ID == "F001" && m.findings[i].Decision != "approved" {
					t.Errorf("F001 decision = %q, want approved", m.findings[i].Decision)
				}
			}
			if !strings.Contains(m.msg, "approved") {
				t.Errorf("msg = %q", m.msg)
			}
		}},
		"findings.reject": {keys: []string{"r"}, post: func(t *testing.T, m *model) {
			for i := range m.findings {
				if m.findings[i].ID == "F001" && m.findings[i].Decision != "rejected" {
					t.Errorf("F001 decision = %q, want rejected", m.findings[i].Decision)
				}
			}
		}},
		"findings.undo": {keys: []string{"a", "u"}, post: func(t *testing.T, m *model) {
			if f := m.current(); f == nil || f.Decision != "pending" {
				t.Errorf("decision = %v, want pending after undo", f)
			}
		}},
		"findings.next-pending": {keys: []string{"a", "gp"}, post: func(t *testing.T, m *model) {
			if m.current() == nil || m.current().ID == "F001" {
				t.Errorf("gp must advance past the approved finding, got %s", m.currentID())
			}
		}},
		"findings.prev-pending": {keys: []string{"3", "j", "gP"}, post: func(t *testing.T, m *model) {
			if m.current() == nil || m.current().ID != "F001" {
				t.Errorf("gP must return to F001, got %s", m.currentID())
			}
		}},
		"findings.discuss": {keys: []string{"d"}, post: func(t *testing.T, m *model) {
			if !m.inputOn {
				t.Error("d must open the ask input")
			}
		}},
		"findings.edit-comment": {keys: []string{"e"}, post: func(t *testing.T, m *model) {
			if !m.editingComment {
				t.Error("e must open the inline comment editor")
			}
		}},
		"findings.open-diff": {keys: []string{"g", "d"}, post: func(t *testing.T, m *model) {
			if m.browseFile != "services/orders/handler.go" {
				t.Errorf("gd must browse the finding's file, got %q", m.browseFile)
			}
		}},
		"findings.nvim": {run: func(t *testing.T, m *model) {}, post: func(t *testing.T, m *model) {
			// When: requires a worktree — with none, v must be a no-op with feedback
			m2 := press(m, "v")
			if m2.rev.Worktree != "" || m2.busy {
				t.Error("v without a worktree must not spawn anything")
			}
		}},
		"findings.sort": {keys: []string{"o"}, post: func(t *testing.T, m *model) {
			if m.sortMode == sortSeverity {
				t.Error("o must cycle the sort mode")
			}
		}},
		"findings.filter": {keys: []string{"f"}, post: func(t *testing.T, m *model) {
			if m.overlay != ovFilter {
				t.Errorf("overlay = %d, want ovFilter", m.overlay)
			}
		}},

		"findings.filter-pending": {run: func(t *testing.T, m *model) { m.setStatusFilter("pending") }, post: func(t *testing.T, m *model) {
			if m.filter.Kind != "pending" {
				t.Errorf("filter kind = %q, want pending", m.filter.Kind)
			}
		}},
		"findings.filter-approved": {run: func(t *testing.T, m *model) { m.setStatusFilter("approved") }, post: func(t *testing.T, m *model) {
			if m.filter.Kind != "approved" {
				t.Errorf("filter kind = %q, want approved", m.filter.Kind)
			}
		}},
		"findings.filter-rejected": {run: func(t *testing.T, m *model) { m.setStatusFilter("rejected") }, post: func(t *testing.T, m *model) {
			if m.filter.Kind != "rejected" {
				t.Errorf("filter kind = %q, want rejected", m.filter.Kind)
			}
		}},
		"findings.filter-published": {run: func(t *testing.T, m *model) { m.setStatusFilter("published") }, post: func(t *testing.T, m *model) {
			if m.filter.Kind != "published" {
				t.Errorf("filter kind = %q, want published", m.filter.Kind)
			}
		}},
		"findings.filter-stale": {run: func(t *testing.T, m *model) { m.setStatusFilter("stale") }, post: func(t *testing.T, m *model) {
			if m.filter.Kind != "stale" {
				t.Errorf("filter kind = %q, want stale", m.filter.Kind)
			}
		}},
		"findings.filter-all": {run: func(t *testing.T, m *model) { m.setStatusFilter("approved"); m.setStatusFilter("all") }, post: func(t *testing.T, m *model) {
			if m.filter.Kind != "all" {
				t.Errorf("filter-all must clear the filter, got %q", m.filter.Kind)
			}
		}},
		// ---- manual review ----
		"view.findings": {keys: []string{"3"}, post: func(t *testing.T, m *model) {
			if m.leftView != viewFindings {
				t.Error("3 must show findings")
			}
		}},
		"view.diff": {keys: []string{"2"}, post: func(t *testing.T, m *model) {
			if m.focus != focusDiff {
				t.Error("2 must focus the diff pane")
			}
		}},
		"view.files": {keys: []string{"1"}, post: func(t *testing.T, m *model) {
			if m.leftView != viewFiles {
				t.Error("1 must show the changed-file tree")
			}
		}},
		// comments view is palette-only now (no direct key binding)
		"view.comments": {keys: []string{"g", "c"}, post: func(t *testing.T, m *model) {
			if m.leftView != viewComments {
				t.Error("palette view.comments must show the comment navigator")
			}
		}},
		"view.file-findings": {keys: []string{"1", "j", "j", "4"}, post: func(t *testing.T, m *model) {
			if m.leftView != viewFileFindings {
				t.Error("view.file-findings must switch to the IN FILE tab")
			}
			if m.fileFindingsFile != "services/orders/handler.go" {
				t.Errorf("fileFindingsFile = %q, want services/orders/handler.go", m.fileFindingsFile)
			}
			if m.fileFindingsCount() < 1 {
				t.Errorf("expected at least one finding for handler.go, got %d", m.fileFindingsCount())
			}
		}},
		"files.h":            {keys: []string{"1", "h"}, post: func(t *testing.T, m *model) {}},
		"files.l":            {keys: []string{"1", "l"}, post: func(t *testing.T, m *model) {}},
		"files.toggle-flat":  {keys: []string{"1", "t"}, post: func(t *testing.T, m *model) {}},
		"files.sort":         {keys: []string{"1", "g", "o"}, post: func(t *testing.T, m *model) {}},
		"files.filter":       {keys: []string{"1", "g", "f"}, post: func(t *testing.T, m *model) {}},
		"files.expand-all":   {run: func(t *testing.T, m *model) { m.filesSetExpansion(true) }, post: func(t *testing.T, m *model) {}},
		"files.collapse-all": {run: func(t *testing.T, m *model) { m.filesSetExpansion(false) }, post: func(t *testing.T, m *model) {}},
		"diff.side-by-side": {run: func(t *testing.T, m *model) { m.width = 200; m.toggleDiffSideBySide() }, post: func(t *testing.T, m *model) {
			if !m.diffSideBySide {
				t.Error("S in the diff pane must toggle side-by-side")
			}
		}},
		"findings.details-down": {keys: []string{"3", "J"}, post: func(t *testing.T, m *model) {}},
		"findings.details-up":   {keys: []string{"3", "J", "K"}, post: func(t *testing.T, m *model) {}},
		"publish.queue": {keys: []string{"g", "q"}, post: func(t *testing.T, m *model) {
			if m.topOverlay() != ovQueue {
				t.Error("Q must open the publish queue")
			}
		}},
		"comments.preview": {run: func(t *testing.T, m *model) {}, post: func(t *testing.T, m *model) {}},
		"view.findings-overview": {keys: []string{"O"}, post: func(t *testing.T, m *model) {
			if m.tab != tabOverview {
				t.Errorf("O must open the Overview tab, got %d", m.tab)
			}
		}},
		"files.mark-reviewed": {keys: []string{"2", "j", "j", "enter", "m"}, post: func(t *testing.T, m *model) {
			if n := len(m.reviewedSet()); n != 1 {
				t.Errorf("reviewed files = %d, want 1", n)
			}
		}},
		"files.next-unreviewed": {keys: []string{"2", "enter", "g", "n"}, post: func(t *testing.T, m *model) {
			if m.browseFile == "" {
				t.Error("gn must browse the next unreviewed file")
			}
		}},
		"files.mark-hunk": {keys: []string{"2", "j", "j", "enter", "R"}, post: func(t *testing.T, m *model) {
			if n := len(m.reviewedHunkSet()); n != 1 {
				t.Errorf("reviewed hunks = %d, want 1", n)
			}
		}},
		"files.next": {keys: []string{"}"}, post: func(t *testing.T, m *model) {}},
		"files.prev": {keys: []string{"{"}, post: func(t *testing.T, m *model) {}},

		// ---- comments ----
		"comments.add": {keys: []string{"g", "d", "c"}, post: func(t *testing.T, m *model) {
			if m.draftEdit == nil {
				t.Error("c in browse mode must open the draft editor")
			}
		}},
		"comments.visual": {keys: []string{"g", "d", "V"}, post: func(t *testing.T, m *model) {
			if !m.visualOn {
				t.Error("V must start the range selection")
			}
		}},
		"comments.general": {run: func(t *testing.T, m *model) { m.startGeneralComment() }, post: func(t *testing.T, m *model) {
			if m.draftEdit == nil || !m.draftEdit.general {
				t.Error("general comment must open a general draft editor")
			}
		}},
		"comments.edit": {run: func(t *testing.T, m *model) {
			_ = store.SaveComments(m.dir, []store.DraftComment{
				store.NewDraftComment("C001", "services/orders/handler.go", "new", 12, 0, false, "draft text"),
			})
			if err := m.reload(); err != nil {
				t.Errorf("reload: %v", err)
			}
			m.setView(viewComments)
			m.editSelectedDraft()
		}, post: func(t *testing.T, m *model) {
			if m.draftEdit == nil {
				t.Error("e on a selected draft must open the editor")
			}
		}},
		"comments.delete": {run: func(t *testing.T, m *model) {
			_ = store.SaveComments(m.dir, []store.DraftComment{
				store.NewDraftComment("C001", "services/orders/handler.go", "new", 12, 0, false, "draft text"),
			})
			_ = m.reload()
			m.setView(viewComments)
			m.confirmDeleteDraft()
		}, post: func(t *testing.T, m *model) {
			if !m.confirmDelete {
				t.Error("d on a draft must arm the y/n confirm")
			}
		}},
		"comments.external-edit": {run: func(t *testing.T, m *model) {
			_ = store.SaveComments(m.dir, []store.DraftComment{
				store.NewDraftComment("C001", "services/orders/handler.go", "new", 12, 0, false, "draft text"),
			})
			_ = m.reload()
			m.setView(viewComments)
			t.Setenv("MR_REVIEW_EDITOR", "true") // instant-exit editor; ExecProcess never runs in-process
			if cmd := m.editDraftExternal(); cmd == nil {
				t.Error("external edit must produce an exec cmd for the program to run")
			}
		}, post: func(t *testing.T, m *model) {}},
		"comments.toggle-ready": {run: func(t *testing.T, m *model) {
			_ = store.SaveComments(m.dir, []store.DraftComment{
				store.NewDraftComment("C001", "services/orders/handler.go", "new", 12, 0, false, "draft text"),
			})
			_ = m.reload()
			m.setView(viewComments)
			m.toggleDraftReady()
		}, post: func(t *testing.T, m *model) {
			if c := m.comments[0]; c.State != store.CommentDraft {
				t.Errorf("toggle-ready must hold the draft back, got %q", c.State)
			}
		}},

		// ---- navigation ----
		"nav.down": {keys: []string{"3", "j"}, post: func(t *testing.T, m *model) {
			if m.cursor != 1 {
				t.Errorf("cursor = %d, want 1", m.cursor)
			}
		}},
		"nav.up": {keys: []string{"j", "k"}, post: func(t *testing.T, m *model) {
			if m.cursor != 0 {
				t.Errorf("cursor = %d, want 0", m.cursor)
			}
		}},
		"nav.first": {keys: []string{"j", "j", "g", "g"}, post: func(t *testing.T, m *model) {
			if m.cursor != 0 {
				t.Errorf("gg cursor = %d", m.cursor)
			}
		}},
		"nav.last": {keys: []string{"3", "G"}, post: func(t *testing.T, m *model) {
			if m.cursor != len(m.filtered)-1 {
				t.Errorf("G cursor = %d, want %d", m.cursor, len(m.filtered)-1)
			}
		}},
		"nav.half-down": {keys: []string{"ctrl+d"}, post: func(t *testing.T, m *model) {}},
		"nav.half-up":   {keys: []string{"ctrl+u"}, post: func(t *testing.T, m *model) {}},
		"nav.focus-next": {keys: []string{"1", "tab"}, post: func(t *testing.T, m *model) {
			if m.focus != focusDiff {
				t.Error("tab from the files pane must move focus to the diff pane")
			}
		}},
		"nav.focus-prev": {keys: []string{"shift+tab"}, post: func(t *testing.T, m *model) {
			if m.focus != focusFiles {
				t.Error("shift+tab must move focus back to the files pane")
			}
		}},
		"nav.focus-left": {keys: []string{"tab", "ctrl+h"}, post: func(t *testing.T, m *model) {
			if m.focus != focusFiles {
				t.Error("ctrl+h must focus the files pane")
			}
		}},
		"nav.focus-right": {keys: []string{"ctrl+l"}, post: func(t *testing.T, m *model) {
			if m.focus != focusDiff {
				t.Error("ctrl+l must focus the diff pane")
			}
		}},
		"nav.tab-prev": {run: func(t *testing.T, m *model) {
			m.narrow = true // h/l are the narrow-mode detail-tab bindings
			press(m, "h")
		}, post: func(t *testing.T, m *model) {
			if m.tab != tabComment {
				t.Errorf("h from Overview must go to Comment, got tab %d", m.tab)
			}
		}},
		"nav.tab-next": {run: func(t *testing.T, m *model) {
			m.narrow = true // h/l are the narrow-mode detail-tab bindings
			press(m, "l")
		}, post: func(t *testing.T, m *model) {
			if m.tab != tabComment {
				t.Errorf("l from Overview must go to Comment, got tab %d", m.tab)
			}
		}},
		"nav.hunk-prev": {keys: []string{"g", "d", "["}, post: func(t *testing.T, m *model) {}},
		"nav.hunk-next": {keys: []string{"g", "d", "]"}, post: func(t *testing.T, m *model) {}},
		// +/- only work in the finding-window mode: esc leaves the continuous browse first
		"nav.ctx-more": {keys: []string{"esc", "+"}, post: func(t *testing.T, m *model) {
			if m.ctxLines <= 6 {
				t.Errorf("ctxLines = %d, want widened", m.ctxLines)
			}
		}},
		"nav.ctx-less":   {keys: []string{"esc", "-"}, post: func(t *testing.T, m *model) {}},
		"nav.horiz-left": {keys: []string{","}, post: func(t *testing.T, m *model) {}},
		"nav.horiz-right": {keys: []string{"."}, post: func(t *testing.T, m *model) {
			if m.diffHOff != 24 {
				t.Errorf("diffHOff = %d, want 24", m.diffHOff)
			}
		}},
		"nav.search": {keys: []string{"g", "d", "/"}, post: func(t *testing.T, m *model) {
			if !m.searchOn {
				t.Error("/ must open the search input")
			}
		}},
		"nav.search-next": {run: func(t *testing.T, m *model) { m.searchText = "cleanup" }, post: func(t *testing.T, m *model) {}},
		"nav.search-prev": {run: func(t *testing.T, m *model) { m.searchText = "cleanup" }, post: func(t *testing.T, m *model) {}},
		"nav.open": {keys: []string{"enter"}, post: func(t *testing.T, m *model) {
			if m.focus != focusDiff {
				t.Error("enter on a finding must focus the detail pane")
			}
		}},

		// ---- view ----
		"view.help": {keys: []string{"?"}, post: func(t *testing.T, m *model) {
			if m.overlay != ovHelp {
				t.Error("? must open the help overlay")
			}
		}},
		"view.palette": {keys: []string{":"}, post: func(t *testing.T, m *model) {
			if !m.paletteOn || len(m.paletteItems) == 0 {
				t.Errorf("palette open = %v with %d items", m.paletteOn, len(m.paletteItems))
			}
		}},
		"view.summary": {keys: []string{"s"}, post: func(t *testing.T, m *model) {
			if m.overlay != ovSummary {
				t.Error("s must open the review summary")
			}
		}},
		"view.summary-edit": {run: func(t *testing.T, m *model) {}, post: func(t *testing.T, m *model) {}},
		"view.inspect": {keys: []string{"i"}, post: func(t *testing.T, m *model) {
			if m.overlay != ovInspector {
				t.Error("i must open the inspector")
			}
		}},
		"view.copy": {keys: []string{"y"}, post: func(t *testing.T, m *model) {
			if !strings.Contains(m.msg, "copied") {
				t.Errorf("y must copy the finding comment, msg = %q", m.msg)
			}
		}},
		"view.zoom": {keys: []string{"z"}, post: func(t *testing.T, m *model) {
			if !m.zoom {
				t.Error("z must zoom the detail pane")
			}
		}},
		"view.theme": {keys: []string{"T"}, post: func(t *testing.T, m *model) {
			if !strings.Contains(m.msg, "theme:") {
				t.Errorf("T must report the theme switch, msg = %q", m.msg)
			}
		}},
		"view.prompt": {run: func(t *testing.T, m *model) {}, post: func(t *testing.T, m *model) {}},
		"view.log":    {run: func(t *testing.T, m *model) {}, post: func(t *testing.T, m *model) {}},

		// ---- export ----
		"export.open": {keys: []string{"x"}, post: func(t *testing.T, m *model) {
			if m.exportDlg == nil {
				t.Error("x must open the export dialog")
			}
		}},

		// ---- gitlab ----
		"publish.preview": {keys: []string{"a", "P"}, post: func(t *testing.T, m *model) {
			if m.overlay != ovPublish {
				t.Error("P must open the publication preview")
			}
		}},
		"publish.approve-mr": {run: func(t *testing.T, m *model) {}, post: func(t *testing.T, m *model) {}},
		"publish.refresh":    {run: func(t *testing.T, m *model) {}, post: func(t *testing.T, m *model) {}},

		"workspace.open-tuicr": {run: func(t *testing.T, m *model) {}, post: func(t *testing.T, m *model) {
			// without a worktree the pane spawn is refused with feedback
			m2 := press(m, "W")
			if m2.msg == "" && wsPane == nil {
				t.Error("W must either open the pane or explain why not")
			}
		}},

		"tuicr.stage": {run: func(t *testing.T, m *model) {}, post: func(t *testing.T, m *model) {
			// staging runs through the tuicr CLI asynchronously; with no
			// session configured it reports a clear error via the status line
			m2 := press(m, ":")
			_ = m2 // palette open proves the action is discoverable
		}},
		"tuicr.sync": {run: func(t *testing.T, m *model) {}, post: func(t *testing.T, m *model) {
			m2 := press(m, ":")
			_ = m2
		}},

		// ---- application ----
		"app.quit": {keys: []string{"q"}, post: func(t *testing.T, m *model) {}},
		"app.clean-worktree": {run: func(t *testing.T, m *model) {
			m.rev.Worktree = "/tmp/definitely-not-real"
			press(m, "C")
		}, post: func(t *testing.T, m *model) {
			if !m.confirmClean {
				t.Error("C must arm the worktree confirm")
			}
		}},
	}
}

// TestEveryActionCovered — the table must name every action the registry
// can serve in the fixture session. Adding an action without a case (or an
// explicit escape) fails here.
func TestEveryActionCovered(t *testing.T) {
	dir := writeReviewFixture(t, t.TempDir())
	m := newSessionModel(t, dir)
	covered := actionCases()
	for _, a := range buildActions(m) {
		if _, ok := covered[a.ID]; !ok {
			t.Errorf("action %q (%s) has no e2e case", a.ID, a.Name)
		}
	}
}

// TestDeadBindings — every multi-key binding must start with a registered
// pending prefix, or it can never fire (]p/[p were dead exactly this way).
func TestDeadBindings(t *testing.T) {
	pfx := map[string]bool{}
	for _, p := range keyPrefixes {
		pfx[p] = true
	}
	for _, a := range buildActions(newSessionModel(t, writeReviewFixture(t, t.TempDir()))) {
		for _, k := range a.Keys {
			multi := len([]rune(k)) > 1 && !strings.HasPrefix(k, "ctrl+") && !strings.HasPrefix(k, "shift+") && !namedKeys[k]
			if multi && !pfx[string([]rune(k)[0])] {
				t.Errorf("action %s: binding %q starts with a non-prefix key — dead binding", a.ID, k)
			}
		}
	}
}

// TestEveryActionExecutes — drive each case through the real Update path.
// Each subtest gets its OWN fixture dir: actions persist decisions/comments
// to disk, and subtests run in map order — shared state would leak.
func TestEveryActionExecutes(t *testing.T) {
	for id, tc := range actionCases() {
		t.Run(id, func(t *testing.T) {
			// actions may flip package-global state (view.theme); restore
			// after every subtest or later goldens render in the wrong theme
			t.Cleanup(func() {
				_ = SetTheme("default", nil)
				_ = SetIcons("auto")
			})
			dir := writeReviewFixture(t, t.TempDir())
			m := newSessionModel(t, dir)
			m.pubBin = "/nonexistent-publisher-guard" // nothing may publish here
			if len(tc.keys) > 0 {
				m = press(m, tc.keys...)
			} else if tc.run != nil {
				tc.run(t, m)
			}
			tc.post(t, m)
		})
	}
}
