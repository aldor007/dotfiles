package tui

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"mr-review/internal/store"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// aiSnapshot rewrites the review dir with a fresh AI snapshot: same IDs as
// the human-reviewed set (a re-run over existing findings) but with every
// human field reset to AI defaults, plus one genuinely new finding.
func aiSnapshot(t *testing.T, dir string, n int) {
	t.Helper()
	fs := mockFindings(n)
	for i := range fs {
		fs[i].Decision = "pending" // AI never carries human state
	}
	nf := mockFindings(1)[0]
	nf.ID = "F009"
	fs = append(fs, nf)
	if err := store.SaveFindings(dir, fs); err != nil {
		t.Fatal(err)
	}
}

// testFindingByID returns the finding with the given stable ID (nil if
// absent). Named distinctly from actions.go's dir-based findingByID.
func testFindingByID(fs []store.Finding, id string) *store.Finding {
	for i := range fs {
		if fs[i].ID == id {
			return &fs[i]
		}
	}
	return nil
}

// TestSetDecisionDoesNotClobberAIWrite — review P1. The TUI's in-memory
// slice is a snapshot taken before the background AI worker appended F003
// to findings.jsonl. setDecision must persist through the store
// transaction (mutating only the matching stored record by ID): the
// decision lands AND the AI's finding survives. The old whole-file write
// of m.findings would have wiped F003.
func TestSetDecisionDoesNotClobberAIWrite(t *testing.T) {
	fs := mockFindings(2)
	m := newTestModel(t, 120, 35, fs) // model memory + disk: F001, F002
	// background worker appends a third finding after the TUI loaded
	nf := mockFindings(1)[0]
	nf.ID = "F003"
	if err := store.SaveFindings(m.dir, append(append([]store.Finding{}, fs...), nf)); err != nil {
		t.Fatal(err)
	}

	m2 := pressKey(m, "a") // approve the selected finding
	if m2.msg == "" || strings.HasPrefix(m2.msg, "save failed") {
		t.Fatalf("setDecision save failed: %q", m2.msg)
	}
	sel := testFindingByID(m2.findings, "F001")
	if sel == nil || sel.Decision != "approved" {
		t.Fatalf("in-memory decision not applied: %+v", sel)
	}
	got, err := store.LoadFindings(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d findings on disk, want 3 — setDecision clobbered the AI write", len(got))
	}
	if f := testFindingByID(got, "F001"); f == nil || f.Decision != "approved" {
		t.Fatalf("F001 decision not persisted: %+v", f)
	}
	if f := testFindingByID(got, "F003"); f == nil || f.Decision != "pending" {
		t.Fatalf("AI finding F003 lost or mutated: %+v", f)
	}
}

// TestCommentEditDoesNotClobberAIWrite — same contract for the Ctrl+S
// comment-editor save.
func TestCommentEditDoesNotClobberAIWrite(t *testing.T) {
	fs := mockFindings(2)
	m := newTestModel(t, 120, 35, fs)
	nf := mockFindings(1)[0]
	nf.ID = "F003"
	if err := store.SaveFindings(m.dir, append(append([]store.Finding{}, fs...), nf)); err != nil {
		t.Fatal(err)
	}

	m.commentTA = textarea.New() // zero-value textarea panics on SetValue
	m.commentTA.SetValue("human edited comment")
	mm, _ := m.updateCommentEditor(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	m2 := mm.(*model)
	if m2.msg == "" || strings.HasPrefix(m2.msg, "save failed") {
		t.Fatalf("comment save failed: %q", m2.msg)
	}
	if m2.findings[0].EditedComment != "human edited comment" {
		t.Fatal("in-memory edit not applied")
	}
	got, err := store.LoadFindings(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d findings on disk, want 3 — comment save clobbered the AI write", len(got))
	}
	if f := testFindingByID(got, "F001"); f == nil || f.EditedComment != "human edited comment" {
		t.Fatalf("edited comment not persisted: %+v", f)
	}
}

// TestReloadReconcileKeepsHumanState — review P1: a re-ingested AI snapshot
// (human fields reset, one new finding) must not wipe the human decision,
// publication, edited comment, discussion, or stale flag the TUI holds.
func TestReloadReconcileKeepsHumanState(t *testing.T) {
	fs := mockFindings(2)
	fs[0].Decision = "approved"
	fs[0].Publication = &store.Publication{DiscussionID: "D7", NoteID: 42}
	fs[0].EditedComment = "human edited comment"
	fs[0].Discussion = []store.DiscussionTurn{{Q: "why?", A: "because"}}
	m := newTestModel(t, 120, 35, fs)
	// the AI worker rewrote the file with human state missing
	aiSnapshot(t, m.dir, 2)

	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	if len(m.findings) != 3 {
		t.Fatalf("%d findings after reload, want 3 (AI snapshot + new F009)", len(m.findings))
	}
	f := testFindingByID(m.findings, "F001")
	if f == nil {
		t.Fatal("F001 missing after reload")
	}
	if f.Decision != "approved" {
		t.Fatalf("AI snapshot wiped the decision: %q", f.Decision)
	}
	if f.Publication == nil || f.Publication.DiscussionID != "D7" {
		t.Fatalf("AI snapshot wiped the publication record: %+v", f.Publication)
	}
	if f.EditedComment != "human edited comment" {
		t.Fatalf("AI snapshot wiped the edited comment: %q", f.EditedComment)
	}
	if len(f.Discussion) != 1 || f.Discussion[0].Q != "why?" {
		t.Fatalf("AI snapshot wiped the discussion: %+v", f.Discussion)
	}
	nf := testFindingByID(m.findings, "F009")
	if nf == nil || nf.Decision != "pending" {
		t.Fatalf("genuinely new AI finding not taken as-is: %+v", nf)
	}
	_, pe, _, pub := counts(t, m)
	if pub != 1 || pe != 2 {
		t.Fatalf("counts after reconcile: pending=%d published=%d, want 2/1", pe, pub)
	}
}

// TestReloadReconcileStaleFlagSurvives — a stale flag on the stored record
// must survive an AI snapshot that reports the finding as fresh, and a
// stale flag only the incoming record carries must still land.
func TestReloadReconcileStaleFlagSurvives(t *testing.T) {
	fs := mockFindings(2)
	fs[0].Stale = true // refresh flagged F001 stale
	m := newTestModel(t, 120, 35, fs)
	if err := store.SaveFindings(m.dir, mockFindings(2)); err != nil {
		t.Fatal(err) // AI snapshot: same IDs, stale flags cleared
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	if f := testFindingByID(m.findings, "F001"); f == nil || !f.Stale {
		t.Fatalf("AI snapshot wiped the stale flag on F001: %+v", f)
	}
	if f := testFindingByID(m.findings, "F002"); f == nil || f.Stale {
		t.Fatal("F002 must not inherit a stale flag it never had")
	}

	// reverse direction: only the incoming record is stale → it must land
	fs2 := mockFindings(2)
	fs2[1].Stale = true
	m2 := newTestModel(t, 120, 35, fs2)
	if err := store.SaveFindings(m2.dir, mockFindings(2)); err != nil {
		t.Fatal(err)
	}
	if err := m2.reload(); err != nil {
		t.Fatal(err)
	}
	if f := testFindingByID(m2.findings, "F002"); f == nil || !f.Stale {
		t.Fatal("incoming stale flag lost in reconcile")
	}
}

// TestReconcileFindingsUnit — direct table check of the merge rules,
// including publish-safety: an incoming publication record always wins
// (the publisher wrote it after the TUI's memory was taken).
func TestReconcileFindingsUnit(t *testing.T) {
	stored := mockFindings(2)
	stored[0].Decision = "approved"
	stored[0].Publication = &store.Publication{DiscussionID: "D1"}
	stored[1].Decision = "rejected"
	stored[1].EditedComment = "keep me"

	incoming := mockFindings(2)
	incoming[0].Publication = &store.Publication{DiscussionID: "D2", NoteID: 9} // publisher ran
	incoming[0].Decision = "pending"
	incoming[1].Decision = "pending"

	out := reconcileFindings(stored, incoming)
	if out[0].Publication == nil || out[0].Publication.DiscussionID != "D2" {
		t.Fatalf("incoming publication must win (publish-safety): %+v", out[0].Publication)
	}
	if out[0].Decision != "approved" {
		t.Fatalf("stored decision lost: %q", out[0].Decision)
	}
	if out[1].Decision != "rejected" || out[1].EditedComment != "keep me" {
		t.Fatalf("stored human state lost: %+v", out[1])
	}

	// genuinely new findings pass through untouched
	fresh := store.Finding{ID: "F777", File: "a.go", Line: 1,
		Severity: "P2", Title: "t", Explanation: "e", Evidence: "v", Comment: "c"}
	incoming = append(incoming, fresh)
	out = reconcileFindings(stored, incoming)
	if !reflect.DeepEqual(out[2], fresh) {
		t.Fatalf("new AI finding mutated: %+v", out[2])
	}

	// empty stored or incoming slices: no reconcile
	if got := reconcileFindings(nil, incoming); len(got) != 3 {
		t.Fatal("empty stored slice must pass incoming through")
	}
	if got := reconcileFindings(stored, nil); got != nil {
		t.Fatal("empty incoming slice must stay empty")
	}
}

// TestSetDecisionConcurrentWithUpdateFindings — a human decision and a
// concurrent store transaction (the AI ingest path) must both land, in any
// interleaving, without one overwriting the other.
func TestSetDecisionConcurrentWithUpdateFindings(t *testing.T) {
	fs := mockFindings(3)
	m := newTestModel(t, 120, 35, fs)
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(1)
	go func() { // simulated AI worker: append a finding via the store API
		defer wg.Done()
		<-start
		_ = store.UpdateFindings(m.dir, func(dfs []store.Finding) ([]store.Finding, error) {
			nf := mockFindings(1)[0]
			nf.ID = "F004"
			return append(dfs, nf), nil
		})
	}()
	close(start)
	sel := m.currentID() // before the press: setDecision auto-advances
	m2 := pressKey(m, "a")
	wg.Wait()
	if sel == "" {
		t.Fatal("no finding selected")
	}
	if f := testFindingByID(m2.findings, sel); f == nil || f.Decision != "approved" {
		t.Fatalf("in-memory decision not applied on %s", sel)
	}
	got, err := store.LoadFindings(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("%d findings on disk, want 4 — decision write clobbered the AI append", len(got))
	}
	if f := testFindingByID(got, sel); f == nil || f.Decision != "approved" {
		t.Fatalf("decision lost to the concurrent append: %+v", f)
	}
}
