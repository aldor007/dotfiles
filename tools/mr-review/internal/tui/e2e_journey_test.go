package tui

// The full simulated review journey (Layer 2): open the fixture MR, navigate
// to a finding, browse its diff, write a manual comment, approve another
// finding, export the review to Markdown, reopen the session — and prove
// that NO GitLab write occurs before explicit confirmation.

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
	m.pubBin = stub

	// 1. navigate to the second finding and open its diff
	m = press(m, "j")
	if id := m.currentID(); id != "F002" {
		t.Fatalf("after j: %s, want F002", id)
	}
	m = press(m, "g", "d")
	if m.browseFile != "services/kent/worker.go" {
		t.Fatalf("browseFile = %q", m.browseFile)
	}

	// 2. write a manual comment on the diff cursor line
	m = press(m, "c")
	if m.draftEdit == nil {
		t.Fatal("c must open the draft editor in browse mode")
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
	if cs[0].State != store.CommentReady || cs[0].Line == 0 {
		t.Errorf("draft = %+v, want ready with a line number", cs[0])
	}

	// 3. approve the current finding
	m = press(m, "a")
	f := findingByID(m.dir, "F002")
	if f == nil || f.Decision != "approved" {
		t.Fatalf("F002 decision = %v", f)
	}

	// 4. export the review to Markdown through the dialog
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
	if !strings.Contains(markdown, "F002") || !strings.Contains(markdown, "cleanup is not idempotent") {
		t.Error("markdown export must contain the approved finding and the manual comment")
	}

	// 5. reopen the session: decisions and comments persist
	m2 := newSessionModel(t, dir)
	f = findingByID(m2.dir, "F002")
	if f == nil || f.Decision != "approved" {
		t.Fatal("approved decision did not survive the reopen")
	}
	if cs, _ := store.LoadComments(dir); len(cs) != 1 || cs[0].State != store.CommentReady {
		t.Fatal("manual comment did not survive the reopen")
	}

	// 6. publish safety: preview, cancel — the publisher must NEVER run
	if _, err := os.Stat(stubLog); !os.IsNotExist(err) {
		t.Fatal("publisher ran before any confirmation")
	}
	m2.pubBin = stub
	m2 = press(m2, "a") // approve so the preview has content
	m2 = press(m2, "P")
	if m2.overlay != ovPublish {
		t.Fatal("P must open the publication preview")
	}
	if _, err := os.Stat(stubLog); !os.IsNotExist(err) {
		t.Fatal("publisher ran on preview")
	}
	m2 = press(m2, "esc") // cancel
	if m2.overlay == ovPublish {
		t.Fatal("esc must cancel the preview")
	}
	if _, err := os.Stat(stubLog); !os.IsNotExist(err) {
		t.Fatal("publisher ran on cancel")
	}

	// 7. explicit confirmation hands off to the publisher binary — the exact
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
