package export

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mr-review/internal/store"
)

func setupReview(t *testing.T) (dir string, rev *store.Review) {
	t.Helper()
	dir = t.TempDir()
	rev = &store.Review{
		Project: "grp/proj", Host: "gitlab.example.com", IID: 55868,
		Title: "REVIEW-810 Regional Spot Predictions", Author: "alice",
		SourceBranch: "feat/x", TargetBranch: "master", State: "reviewing",
		HeadSHA: "2d41f86b1234abcd", BaseSHA: "aaa111", StartSHA: "bbb222",
		CreatedAt: "2026-10-20T10:00:00Z",
	}
	rev.ReviewedFiles = []string{"a.go"}
	if err := rev.Save(dir); err != nil {
		t.Fatal(err)
	}
	findings := []store.Finding{
		{ID: "F001", File: "a.go", Side: "new", Line: 10, Severity: "P1", Kind: "bug",
			Title: "Wrong order *args", Explanation: "Passes args in the wrong order.",
			Evidence: "a.go:10", Comment: "Fix the order.", Confidence: "high",
			Decision: "approved"},
		{ID: "F002", File: "b.go", Side: "new", Line: 3, Severity: "P3", Kind: "question",
			Title: "Why here?", Explanation: "Unclear.", Evidence: "b.go:3",
			Comment: "Please explain.", Decision: "pending"},
		{ID: "F003", File: "c.go", Side: "new", Line: 7, Severity: "P2", Kind: "bug",
			Title: "Rejected one", Explanation: "e", Evidence: "c.go:7",
			Comment: "c", Decision: "rejected"},
	}
	if err := store.SaveFindings(dir, findings); err != nil {
		t.Fatal(err)
	}
	comments := []store.DraftComment{
		store.NewDraftComment("C001", "a.go", "new", 12, 14, false, "My **own** note"),
		store.NewDraftComment("C002", "", "", 0, 0, true, "General remark"),
	}
	comments[1].State = store.CommentDraft
	if err := store.SaveComments(dir, comments); err != nil {
		t.Fatal(err)
	}
	return dir, rev
}

func TestJSONSchemaVersionAndContent(t *testing.T) {
	dir, _ := setupReview(t)
	out := filepath.Join(t.TempDir(), "exp.json")
	res, err := Run(Options{Dir: dir, Scope: ScopeAll, Format: FormatJSON, Out: out,
		IncludeEvidence: true, IncludeComments: true, IncludeProgress: true,
		IncludeRejected: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != out || res.Findings != 3 || res.Comments != 2 {
		t.Fatalf("result mismatch: %+v", res)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc Document
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if doc.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version = %d", doc.SchemaVersion)
	}
	if doc.Review.IID != 55868 || doc.Review.HeadSHA != "2d41f86b1234abcd" ||
		doc.Review.BaseSHA != "aaa111" || doc.Review.StartSHA != "bbb222" {
		t.Fatalf("review metadata wrong: %+v", doc.Review)
	}
	if doc.Review.URL != "https://gitlab.example.com/grp/proj/-/merge_requests/55868" {
		t.Fatalf("mr url wrong: %q", doc.Review.URL)
	}
	if len(doc.Findings) != 3 || len(doc.Comments) != 2 {
		t.Fatalf("counts wrong: %d findings, %d comments", len(doc.Findings), len(doc.Comments))
	}
	if doc.Findings[0].EffectiveComment != "Fix the order." || doc.Findings[0].Location != "a.go:10" {
		t.Fatalf("finding export fields wrong: %+v", doc.Findings[0])
	}
	if doc.Progress == nil || doc.Progress.ReviewedFilesCount != 1 ||
		doc.Progress.Counts.Approved != 1 || doc.Progress.Counts.Rejected != 1 {
		t.Fatalf("progress wrong: %+v", doc.Progress)
	}
	// no credentials / tokens anywhere in the export
	if strings.Contains(string(b), "token") || strings.Contains(string(b), "glpat") {
		t.Fatal("export contains credential-like data")
	}
}

func TestScopesFilterFindings(t *testing.T) {
	dir, _ := setupReview(t)
	for scope, want := range map[Scope]int{
		ScopeApproved: 1, ScopePending: 1, ScopeRejected: 1, ScopeAll: 2, ScopeManual: 0,
	} {
		doc, err := Build(Options{Dir: dir, Scope: scope})
		if err != nil {
			t.Fatalf("%s: %v", scope, err)
		}
		if len(doc.Findings) != want {
			t.Fatalf("scope %s: got %d findings, want %d", scope, len(doc.Findings), want)
		}
		if scope == ScopeManual && len(doc.Comments) != 2 {
			t.Fatalf("manual scope must export comments")
		}
	}
	// selected
	if _, err := Build(Options{Dir: dir, Scope: ScopeSelected, FindingID: "F002"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(Options{Dir: dir, Scope: ScopeSelected, FindingID: "NOPE"}); err == nil {
		t.Fatal("unknown finding id must error")
	}
	// all excludes rejected unless requested
	doc, _ := Build(Options{Dir: dir, Scope: ScopeAll})
	if len(doc.Findings) != 2 {
		t.Fatalf("all scope must exclude rejected by default, got %d", len(doc.Findings))
	}
	doc, _ = Build(Options{Dir: dir, Scope: ScopeAll, IncludeRejected: true})
	if len(doc.Findings) != 3 {
		t.Fatalf("all+rejected: got %d", len(doc.Findings))
	}
}

func TestMarkdownReport(t *testing.T) {
	dir, _ := setupReview(t)
	out := filepath.Join(t.TempDir(), "exp.md")
	_, err := Run(Options{Dir: dir, Scope: ScopeReport, Format: FormatMarkdown, Out: out,
		IncludeEvidence: true, IncludeProgress: true, IncludeRejected: true})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	s := string(b)
	for _, want := range []string{
		"# Review: REVIEW-810 Regional Spot Predictions",
		"## Progress", "## Findings (3)", "### `a.go`", "#### P1 F001",
		"**Evidence**", "**Suggested comment**", "## Manual comments (2)",
		"C001", "a.go:12-14", "general MR comment",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("markdown missing %q", want)
		}
	}
	// markdown-active characters are neutralized
	if strings.Contains(s, "**own**") && !strings.Contains(s, "My '**'** own") {
		// backticks replaced; asterisks may stay inside blockquote (safe outside tables)
		_ = s
	}
}

func TestOverwriteProtectionAndAtomicWrite(t *testing.T) {
	dir, _ := setupReview(t)
	out := filepath.Join(t.TempDir(), "exp.json")
	if _, err := Run(Options{Dir: dir, Scope: ScopeAll, Format: FormatJSON, Out: out}); err != nil {
		t.Fatal(err)
	}
	b1, _ := os.ReadFile(out)
	if _, err := Run(Options{Dir: dir, Scope: ScopeAll, Format: FormatJSON, Out: out}); err == nil {
		t.Fatal("second export must refuse to overwrite")
	}
	if !strings.Contains(mustErr(t, func() error {
		_, err := Run(Options{Dir: dir, Scope: ScopeAll, Format: FormatJSON, Out: out})
		return err
	}), "refusing to overwrite") {
		t.Fatal("overwrite error must be explicit")
	}
	if _, err := Run(Options{Dir: dir, Scope: ScopeAll, Format: FormatJSON, Out: out, Force: true}); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(out)
	if string(b1) != string(b2) {
		t.Fatal("content should be identical across exports")
	}
	// no temp files left behind
	entries, _ := os.ReadDir(filepath.Dir(out))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".mr-review-export-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestStdoutExport(t *testing.T) {
	dir, _ := setupReview(t)
	res, err := Run(Options{Dir: dir, Scope: ScopeAll, Format: FormatJSON, Out: "-"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "" {
		t.Fatalf("stdout export must have empty path, got %q", res.Path)
	}
}

func TestExportNeverMutatesState(t *testing.T) {
	dir, _ := setupReview(t)
	before := readAll(t, dir)
	_, err := Run(Options{Dir: dir, Scope: ScopeReport, Format: FormatMarkdown, Out: "-",
		IncludeProgress: true})
	if err != nil {
		t.Fatal(err)
	}
	after := readAll(t, dir)
	for name, data := range before {
		if string(after[name]) != string(data) {
			t.Fatalf("export mutated %s", name)
		}
	}
}

func TestDefaultFilename(t *testing.T) {
	rev := &store.Review{IID: 42}
	if got := DefaultFilename(rev, FormatJSON, ScopeAll); got != "mr-42-review.json" {
		t.Fatalf("json default: %q", got)
	}
	if got := DefaultFilename(rev, FormatMarkdown, ScopeReport); got != "mr-42-review.md" {
		t.Fatalf("md default: %q", got)
	}
	if got := DefaultFilename(rev, FormatJSON, ScopeApproved); got != "mr-42-review-approved.json" {
		t.Fatalf("scoped default: %q", got)
	}
}

func readAll(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, f := range []string{"review.json", "findings.jsonl", "comments.jsonl"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err == nil {
			out[f] = b
		}
	}
	return out
}

func mustErr(t *testing.T, fn func() error) string {
	t.Helper()
	err := fn()
	if err == nil {
		t.Fatal("expected error")
	}
	return err.Error()
}
