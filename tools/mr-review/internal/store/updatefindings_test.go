package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// updateSeed returns three valid findings (F001..F003).
func updateSeed() []Finding {
	fs := make([]Finding, 0, 3)
	for i := 1; i <= 3; i++ {
		f := validFinding()
		f.ID = fmt.Sprintf("F%03d", i)
		f.Title = fmt.Sprintf("finding %d", i)
		fs = append(fs, f)
	}
	return fs
}

// TestUpdateFindingsTransaction — the whole point of the API: the mutator
// runs on the STORED slice under the lock, so a mutation made against a
// stale in-memory copy still lands on (and preserves) the current disk
// state. With a blind read-then-SaveFindings this would drop F003.
func TestUpdateFindingsTransaction(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFindings(dir, updateSeed()); err != nil {
		t.Fatal(err)
	}
	// stale snapshot: F001 only, as the TUI might hold it
	stale := updateSeed()[:1]

	err := UpdateFindings(dir, func(fs []Finding) ([]Finding, error) {
		if len(fs) != 3 {
			t.Errorf("mutator saw %d findings, want the 3 stored ones", len(fs))
		}
		for i := range fs {
			if fs[i].ID == "F001" {
				fs[i].Decision = "approved"
			}
		}
		return fs, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadFindings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("stored %d findings, want 3 (F003 must survive the stale snapshot)", len(got))
	}
	if got[0].ID != "F001" || got[0].Decision != "approved" {
		t.Fatalf("F001 decision not persisted: %+v", got[0])
	}
	_ = stale // the snapshot above is what the mutator's argument replaces
}

// TestUpdateFindingsConcurrentMutations — deterministic lost-update check
// (review P1): two goroutines run read-modify-write transactions with
// different mutations (one approves F001, one appends a new finding). Both
// effects must survive; there is no last-writer-wins because each
// transaction sees the other's committed state. No sleeps: goroutines are
// released together by a closed barrier channel and joined via WaitGroup.
func TestUpdateFindingsConcurrentMutations(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFindings(dir, updateSeed()); err != nil {
		t.Fatal(err)
	}
	const rounds = 8
	for r := 0; r < rounds; r++ {
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 2)
		wg.Add(2)
		go func() { // approves F001
			defer wg.Done()
			<-start
			errs[0] = UpdateFindings(dir, func(fs []Finding) ([]Finding, error) {
				for i := range fs {
					if fs[i].ID == "F001" {
						fs[i].Decision = "approved"
					}
				}
				return fs, nil
			})
		}()
		go func() { // appends a new finding
			defer wg.Done()
			<-start
			errs[1] = UpdateFindings(dir, func(fs []Finding) ([]Finding, error) {
				nf := validFinding()
				nf.ID = fmt.Sprintf("F1%02d", r)
				return append(fs, nf), nil
			})
		}()
		close(start)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d writer %d: %v", r, i, err)
			}
		}
		got, err := LoadFindings(dir)
		if err != nil {
			t.Fatalf("round %d: %v", r, err)
		}
		if len(got) != 3+r+1 {
			t.Fatalf("round %d: %d findings on disk, want %d — a transaction was lost", r, len(got), 4+r)
		}
		decided := false
		for _, f := range got {
			if f.ID == "F001" && f.Decision == "approved" {
				decided = true
			}
		}
		if !decided {
			t.Fatalf("round %d: F001 approval lost", r)
		}
	}
}

// TestUpdateFindingsParallelAppends — N goroutines each append one finding
// under the lock; all N must land. Under whole-file last-writer-wins all
// but one would vanish.
func TestUpdateFindingsParallelAppends(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFindings(dir, updateSeed()); err != nil {
		t.Fatal(err)
	}
	const n = 8
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = UpdateFindings(dir, func(fs []Finding) ([]Finding, error) {
				nf := validFinding()
				nf.ID = fmt.Sprintf("F2%02d", i)
				return append(fs, nf), nil
			})
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	got, err := LoadFindings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3+n {
		t.Fatalf("%d findings on disk, want %d", len(got), 3+n)
	}
	seen := map[string]bool{}
	for _, f := range got {
		if seen[f.ID] {
			t.Fatalf("duplicate id %s", f.ID)
		}
		seen[f.ID] = true
	}
	for i := 0; i < n; i++ {
		if !seen[fmt.Sprintf("F2%02d", i)] {
			t.Fatalf("appended finding F2%02d missing", i)
		}
	}
}

// TestUpdateFindingsMutatorErrorAborts — an fn error leaves the file
// untouched.
func TestUpdateFindingsMutatorErrorAborts(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFindings(dir, updateSeed()); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if err := UpdateFindings(dir, func(fs []Finding) ([]Finding, error) {
		return nil, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	got, err := LoadFindings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("file modified by aborted transaction: %d findings", len(got))
	}
}

// TestUpdateFindingsValidatesFailClosed — a mutator producing an invalid
// record must not poison findings.jsonl (LoadFindings is fail-closed: one
// bad line aborts every future load).
func TestUpdateFindingsValidatesFailClosed(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFindings(dir, updateSeed()); err != nil {
		t.Fatal(err)
	}
	err := UpdateFindings(dir, func(fs []Finding) ([]Finding, error) {
		for i := range fs {
			if fs[i].ID == "F002" {
				fs[i].Decision = "auto-approved" // invalid
			}
		}
		return fs, nil
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	got, err := LoadFindings(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		if f.ID == "F002" && f.Decision != "pending" {
			t.Fatalf("invalid decision persisted: %q", f.Decision)
		}
	}
}

// TestUpdateFindingsUsesSameLockFile — the transaction and a plain
// whole-file SaveFindings share the advisory lock, so concurrent runs
// never tear the file. Last-writer-wins still applies to a caller that
// reads OUTSIDE the lock (that is why callers must use UpdateFindings);
// the only valid end states are the two writers' complete outputs.
func TestUpdateFindingsUsesSameLockFile(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFindings(dir, updateSeed()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, 2)
	wg.Add(2)
	go func() { // transactional decision
		defer wg.Done()
		<-start
		errs[0] = UpdateFindings(dir, func(fs []Finding) ([]Finding, error) {
			for i := range fs {
				if fs[i].ID == "F001" {
					fs[i].Decision = "approved"
				}
			}
			return fs, nil
		})
	}()
	go func() { // plain whole-file writer appending F004
		defer wg.Done()
		<-start
		fs, err := LoadFindings(dir)
		if err != nil {
			errs[1] = err
			return
		}
		nf := validFinding()
		nf.ID = "F004"
		errs[1] = SaveFindings(dir, append(fs, nf))
	}()
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	got, err := LoadFindings(dir) // load error here would mean a torn write
	if err != nil {
		t.Fatalf("file corrupted by concurrent writers: %v", err)
	}
	// Valid end states: the transaction's full output (3 findings, F001
	// approved) or the whole-file writer's full output (4 findings). Any
	// other combination means the lock funnel failed.
	decided := ""
	for _, f := range got {
		if f.ID == "F001" {
			decided = f.Decision
		}
	}
	switch {
	case len(got) == 3 && decided == "approved": // transaction won
	case len(got) == 4: // whole-file write landed last
	default:
		t.Fatalf("invalid end state: %d findings, F001 decision %q", len(got), decided)
	}
}
