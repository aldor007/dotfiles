package tui

import (
	"strings"
	"testing"

	"mr-review/internal/store"
)

func f(sev, id, file string, line int, decision string) store.Finding {
	return store.Finding{
		ID: id, File: file, Line: line, LineEnd: line, Severity: sev,
		Kind: "bug", Title: "t-" + id, Explanation: "e", Evidence: "v",
		Comment: "c", Decision: decision,
	}
}

func TestFilterMatching(t *testing.T) {
	fs := []store.Finding{
		f("P1", "F001", "a.go", 1, "pending"),
		f("P0", "F002", "b.go", 2, "approved"),
	}
	pub := f("P2", "F003", "c.go", 3, "approved")
	pub.Publication = &store.Publication{DiscussionID: "d1"}
	fs = append(fs, pub)

	if !(Filter{Kind: "all"}).Match(&fs[0]) {
		t.Fatal("all must match pending")
	}
	if (Filter{Kind: "pending"}).Match(&fs[1]) {
		t.Fatal("pending must not match approved")
	}
	if !(Filter{Kind: "approved"}).Match(&fs[1]) {
		t.Fatal("approved must match")
	}
	if !(Filter{Kind: "published"}).Match(&fs[2]) {
		t.Fatal("published must match publication")
	}
	if (Filter{Kind: "published"}).Match(&fs[1]) {
		t.Fatal("approved-unpublished must not match published filter")
	}
	if !(Filter{Kind: "all", Severity: "P0"}).Match(&fs[1]) {
		t.Fatal("severity filter must match")
	}
	if (Filter{Kind: "all", Severity: "P0"}).Match(&fs[0]) {
		t.Fatal("severity filter must reject other severity")
	}
	if !(Filter{Kind: "all", File: "b."}).Match(&fs[1]) {
		t.Fatal("file substring must match")
	}
}

func TestSortFindingsSeverityThenID(t *testing.T) {
	fs := []store.Finding{
		f("P2", "F009", "a.go", 1, "pending"),
		f("P0", "F002", "a.go", 2, "pending"),
		f("P1", "F005", "b.go", 3, "pending"),
		f("P1", "F003", "b.go", 4, "pending"),
	}
	sortFindings(fs)
	want := []string{"F002", "F003", "F005", "F009"}
	for i, id := range want {
		if fs[i].ID != id {
			t.Fatalf("pos %d: got %s want %s", i, fs[i].ID, id)
		}
	}
}

func TestRenderSummaryCounts(t *testing.T) {
	rev := &store.Review{IID: 5, Title: "T", SourceBranch: "a", TargetBranch: "b", HeadSHA: "abcd12345"}
	fs := []store.Finding{
		f("P1", "F001", "a.go", 1, "approved"),
		f("P1", "F002", "a.go", 2, "pending"),
		f("P2", "F003", "b.go", 3, "rejected"),
	}
	pub := f("P0", "F004", "b.go", 4, "approved")
	pub.Publication = &store.Publication{DiscussionID: "d"}
	fs = append(fs, pub)
	s := RenderSummary(rev, fs, nil)
	for _, want := range []string{"!5", "4 findings", "1 approved", "1 pending", "1 rejected", "1 published"} {
		if !strings.Contains(s, want) {
			t.Fatalf("summary missing %q:\n%s", want, s)
		}
	}
}

func TestWrap(t *testing.T) {
	long := strings.Repeat("word ", 50)
	lines := wrap(long, 40)
	if len(lines) < 5 {
		t.Fatalf("wrap produced too few lines: %d", len(lines))
	}
	for _, l := range lines {
		if len(l) > 40 {
			t.Fatalf("line exceeds width: %q", l)
		}
	}
}
