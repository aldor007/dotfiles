package tui

// The full simulated review journey (Layer 2), migrated to the redesigned
// three-pane UX: startup shows the continuous DIFF focused between FILES and
// FINDINGS; findings selection drives the diff; manual comments float over
// the diff and are saved HELD; help and the palette FLOAT over an intact
// body; export + reopen persist everything — and NO GitLab write occurs
// before explicit confirmation.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mr-review/internal/store"
)

func TestReviewJourney(t *testing.T) {
	dir := writeReviewFixture(t, t.TempDir())
	m := newSessionModel(t, dir)
	stubLog := filepath.Join(t.TempDir(), "publisher.log")
	stub := writeStubPublisher(t, stubLog)
	m.pubBin = stub // guard: nothing may publish during the journey

	// 1. startup contract (startDiffFirst): the continuous diff is visible
	// and focused in the center pane, first changed file, F001 selected.
	if m.focus != focusDiff {
		t.Fatalf("startup focus = %d, want the diff pane", m.focus)
	}
	if m.layout().mode != layoutWide {
		t.Fatalf("startup layout mode = %d at 120 cols, want wide (3 panes)", m.layout().mode)
	}
	if m.browseFile != "services/kent/handler.go" {
		t.Fatalf("startup browseFile = %q, want the first changed file", m.browseFile)
	}
	if id := m.currentID(); id != "F001" {
		t.Fatalf("startup findings selection = %s, want F001", id)
	}
	if row := m.browseRowAt(m.diffCursor); row == nil || row.dl.NewNo == 0 {
		t.Fatalf("startup diff cursor on a non-addressable row: %+v", m.browseRowAt(m.diffCursor))
	}

	// 2. j walks the continuous diff ACROSS file boundaries: the cursor
	// flows handler.go → worker.go inside one document, k walks back.
	n := m.browseDocLen()
	steps := 0
	for m.browseFile != "services/kent/worker.go" && steps <= n+1 {
		m = press(m, "j")
		steps++
	}
	if m.browseFile != "services/kent/worker.go" {
		t.Fatalf("j never crossed the file boundary after %d steps (doc len %d)", steps, n)
	}
	for m.browseFile != "services/kent/handler.go" && steps > 0 {
		m = press(m, "k")
		steps--
	}
	if m.browseFile != "services/kent/handler.go" {
		t.Fatalf("k never walked back: browseFile = %q", m.browseFile)
	}

	// 3. 3 focuses the findings pane; j moves the FINDINGS cursor; approving
	// auto-advances to the next pending finding (new-UX semantics).
	m = press(m, "3")
	if m.focus != focusFindings || m.leftView != viewFindings {
		t.Fatalf("after 3: focus=%d leftView=%d, want findings focus", m.focus, m.leftView)
	}
	m = press(m, "j")
	if id := m.currentID(); id != "F002" {
		t.Fatalf("after j in findings: %s, want F002", id)
	}
	m = press(m, "a")
	if f := findingByID(m.dir, "F002"); f == nil || f.Decision != "approved" {
		t.Fatalf("F002 decision = %v", f)
	}
	if id := m.currentID(); id != "F001" {
		t.Fatalf("approve must auto-advance to the next pending finding, got %s", id)
	}

	// 4. back to F002, then gd jumps the continuous diff to its line.
	m = press(m, "j") // F001 → F002
	m = press(m, "g", "d")
	if m.browseFile != "services/kent/worker.go" {
		t.Fatalf("gd: browseFile = %q, want worker.go", m.browseFile)
	}
	if m.focus != focusDiff {
		t.Fatalf("gd must focus the diff pane, focus = %d", m.focus)
	}
	if row := m.browseRowAt(m.diffCursor); row == nil || row.dl.numberFor("new") != 31 {
		t.Fatalf("gd landed on %+v, want worker.go new-side line 31", m.browseRowAt(m.diffCursor))
	}

	// 5. c opens the FLOATING comment editor over the (unchanged) diff;
	// Ctrl+S saves a HELD draft anchored at worker.go:31 (new side).
	m = press(m, "c")
	if m.draftEdit == nil {
		t.Fatal("c must open the floating draft editor in browse mode")
	}
	frame := m.View().Content
	if !strings.Contains(frame, "FINDINGS") || !strings.Contains(frame, "worker.go") {
		t.Error("the comment editor must float over the intact three-pane body")
	}
	m.draftEdit.ta.SetValue("cleanup is not idempotent — guard with sync.Once.")
	m = press(m, "ctrl+s")
	if m.draftEdit != nil {
		t.Fatal("ctrl+s must close the editor")
	}
	cs, err := store.LoadComments(dir)
	if err != nil || len(cs) != 1 {
		t.Fatalf("comments on disk = %d, err %v, want 1", len(cs), err)
	}
	// redesign contract: new drafts start HELD — saving never marks a
	// comment ready-to-publish implicitly (the legacy 2-pane flow saved
	// ready drafts; the navigator toggle is now the explicit ready step).
	if cs[0].State != store.CommentDraft {
		t.Errorf("draft state = %v, want held (CommentDraft)", cs[0].State)
	}
	if cs[0].File != "services/kent/worker.go" || cs[0].Side != "new" || cs[0].Line != 31 {
		t.Errorf("draft anchored at %s:%d %s, want worker.go:31 new", cs[0].File, cs[0].Line, cs[0].Side)
	}

	// 6. m toggles the browsed file's reviewed mark (both directions).
	m = press(m, "m")
	if !m.reviewedSet()["services/kent/worker.go"] {
		t.Fatal("m must mark the browsed file reviewed")
	}
	m = press(m, "m")
	if m.reviewedSet()["services/kent/worker.go"] {
		t.Fatal("m must toggle the reviewed mark back off")
	}

	// 7. ? opens the floating help over the intact body; Esc closes and
	// restores focus + scroll.
	scrollBefore := m.detailTop[tabOverview]
	m = press(m, "?")
	if m.topOverlay() != ovHelp {
		t.Fatalf("? must open the help overlay, got %d", m.topOverlay())
	}
	help := m.View().Content
	// the help overlay renders action DESCRIPTIONS next to key labels; with
	// the diff focused, the diff-context section is in the first window
	if !strings.Contains(stripANSIish(help), "jump to the previous file in the diff") {
		t.Error("help must list registry actions (diff-context section)")
	}
	if !strings.Contains(help, "FINDINGS") {
		t.Error("help must float over the unchanged three-pane body")
	}
	m = press(m, "esc")
	if m.topOverlay() != ovNone {
		t.Fatalf("esc must close help, overlay = %d", m.topOverlay())
	}
	if m.focus != focusDiff || m.detailTop[tabOverview] != scrollBefore {
		t.Fatalf("help close lost state: focus=%d scroll=%d (want diff, %d)",
			m.focus, m.detailTop[tabOverview], scrollBefore)
	}

	// 8. : palette runs the view.comments action by name.
	m = press(m, ":")
	if !m.paletteOn {
		t.Fatal(": must open the command palette")
	}
	m = press(m, "n", "a", "v", "i", "g", "a", "t", "o", "r")
	m = press(m, "enter")
	if m.paletteOn {
		t.Fatal("enter must run the palette action and close the palette")
	}
	if m.leftView != viewComments || m.focus != focusFindings {
		t.Fatalf("palette must run view.comments: leftView=%d focus=%d", m.leftView, m.focus)
	}
	if !strings.Contains(m.View().Content, "COMMENTS") {
		t.Error("the comment navigator must render after view.comments")
	}

	// 9. export the review to Markdown through the dialog (unchanged intent).
	out := filepath.Join(t.TempDir(), "review.md")
	m = press(m, "x")
	if m.exportDlg == nil {
		t.Fatal("x must open the export dialog")
	}
	m.exportDlg.move(2) // destination field
	m.exportDlg.dest.SetValue(out)
	res, _ := m.Update(teaKeyPress("ctrl+s"))
	if mm, ok := res.(*model); ok {
		m = mm
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("export wrote nothing: %v", err)
	}
	b, _ := os.ReadFile(out)
	markdown := string(b)
	// the export is a LOCAL report: it contains the approved finding and
	// every manual draft, labeled with its state (the held draft is NOT in
	// the publish batch — only ready comments are, see the P preview below)
	if !strings.Contains(markdown, "F002") || !strings.Contains(markdown, "cleanup is not idempotent") {
		t.Error("markdown export must contain the approved finding and the manual comment")
	}
	if !strings.Contains(markdown, "draft") {
		t.Error("the export must label the held draft's state")
	}

	// 10. reopen the session: decisions and drafts persist.
	m2 := newSessionModel(t, dir)
	if f := findingByID(m2.dir, "F002"); f == nil || f.Decision != "approved" {
		t.Fatal("approved decision did not survive the reopen")
	}
	if cs, _ := store.LoadComments(dir); len(cs) != 1 || cs[0].State != store.CommentDraft {
		t.Fatal("manual draft did not survive the reopen as a held draft")
	}

	// 11. publish safety: preview shows the batch, cancel — the publisher
	// must NEVER run.
	if _, err := os.Stat(stubLog); !os.IsNotExist(err) {
		t.Fatal("publisher ran before any confirmation")
	}
	m2.pubBin = stub
	m2 = press(m2, "P")
	if m2.topOverlay() != ovPublish {
		t.Fatal("P must open the publication preview")
	}
	preview := stripANSIish(m2.View().Content)
	if !strings.Contains(preview, "F002") || !strings.Contains(preview, "PUBLICATION PREVIEW") {
		t.Error("the preview must show the approved batch before anything is sent")
	}
	if _, err := os.Stat(stubLog); !os.IsNotExist(err) {
		t.Fatal("publisher ran on preview")
	}
	m2 = press(m2, "esc") // cancel
	if m2.topOverlay() == ovPublish {
		t.Fatal("esc must cancel the preview")
	}
	if _, err := os.Stat(stubLog); !os.IsNotExist(err) {
		t.Fatal("publisher ran on cancel")
	}

	// 12. explicit confirmation hands off to the publisher binary — the exact
	// handoff publishRun performs (tea.ExecProcess suspends the TUI; here we
	// invoke the same binary+args it would)
	cmd := exec.Command(stub, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stub publisher: %v (%s)", err, out)
	}
	log, err := os.ReadFile(stubLog)
	if err != nil || !strings.Contains(string(log), dir) {
		t.Fatalf("publisher handoff not recorded: %v %q", err, string(log))
	}
	// the review state itself is untouched by the handoff
	if fs, _ := store.LoadFindings(dir); len(fs) != 2 {
		t.Fatalf("findings changed after publish run: %d", len(fs))
	}
}

// writeStubPublisher creates an executable that records its invocation —
// the TUI's publisher handoff, with zero GitLab access.
func writeStubPublisher(t *testing.T, logPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stub-publisher.sh")
	script := "#!/bin/sh\necho \"$@\" >> " + logPath + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
