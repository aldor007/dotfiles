package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validFinding() Finding {
	return Finding{
		File: "services/review/handler.go", Line: 10, LineEnd: 12,
		Severity: "P1", Title: "ctx not propagated",
		Explanation: "the context is dropped before the call",
		Evidence:    "handler.go:10 passes context.Background()",
		Comment:     "Please propagate ctx here.",
	}
}

func TestValidateOK(t *testing.T) {
	f := validFinding()
	if err := f.Validate(); err != nil {
		t.Fatalf("valid finding rejected: %v", err)
	}
	if f.Side != "new" || f.Kind != "bug" || f.Confidence != "medium" || f.Decision != "pending" {
		t.Fatalf("defaults not applied: %+v", f)
	}
}

func TestValidateRejections(t *testing.T) {
	cases := map[string]func(*Finding){
		"traversal-dotdot": func(f *Finding) { f.File = "../etc/passwd" },
		"traversal-dot":    func(f *Finding) { f.File = "a/./b.go" },
		"absolute":         func(f *Finding) { f.File = "/etc/passwd" },
		"backslash":        func(f *Finding) { f.File = `a\b.go` },
		"empty-file":       func(f *Finding) { f.File = "  " },
		"bad-severity":     func(f *Finding) { f.Severity = "P9" },
		"bad-line":         func(f *Finding) { f.Line = 0 },
		"missing-evidence": func(f *Finding) { f.Evidence = "" },
		"missing-comment":  func(f *Finding) { f.Comment = "" },
		"missing-title":    func(f *Finding) { f.Title = "" },
		"missing-expl":     func(f *Finding) { f.Explanation = "" },
		"bad-kind":         func(f *Finding) { f.Kind = "nitpick" },
		"bad-confidence":   func(f *Finding) { f.Confidence = "certain" },
		"bad-side":         func(f *Finding) { f.Side = "middle" },
		"bad-decision":     func(f *Finding) { f.Decision = "auto-approved" },
	}
	for name, mut := range cases {
		f := validFinding()
		mut(&f)
		if err := f.Validate(); err == nil {
			t.Errorf("%s: expected rejection, got nil", name)
		}
	}
}

func TestValidateClampsLineEnd(t *testing.T) {
	f := validFinding()
	f.LineEnd = 5 // below Line 10 -> clamped up to Line
	if err := f.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.LineEnd != f.Line {
		t.Fatalf("line_end not clamped: %d", f.LineEnd)
	}
}

func TestFindingsRoundtripAndFailClosed(t *testing.T) {
	dir := t.TempDir()
	fs := []Finding{func() Finding { f := validFinding(); f.ID = "F001"; return f }()}
	if err := SaveFindings(dir, fs); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadFindings(dir)
	if err != nil || len(got) != 1 || got[0].ID != "F001" {
		t.Fatalf("roundtrip: %v %+v", err, got)
	}

	// one corrupt line must abort the whole load (fail closed)
	path := FindingsPath(dir)
	if err := os.WriteFile(path, []byte(
		`{"id":"F001","file":"a.go","line":1,"severity":"P1","title":"t","explanation":"e","evidence":"v","comment":"c"}`+"\n"+
			`{"id":"F002","file":"../evil","line":1,"severity":"P1","title":"t","explanation":"e","evidence":"v","comment":"c"}`+"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFindings(dir); err == nil || !strings.Contains(err.Error(), "unsafe file path") {
		t.Fatalf("expected fail-closed on corrupt line, got %v", err)
	}
}

func TestCommentPrefersEdited(t *testing.T) {
	f := validFinding()
	f.EditedComment = "human wording"
	if f.EffectiveComment() != "human wording" {
		t.Fatal("edited comment must win")
	}
	f.EditedComment = "   "
	if f.EffectiveComment() != "Please propagate ctx here." {
		t.Fatal("blank edit must fall back to agent comment")
	}
}

func TestReviewSaveLoad(t *testing.T) {
	dir := t.TempDir()
	r := &Review{Project: "grp/proj", IID: 7, HeadSHA: "abc", Title: "T", State: "reviewing"}
	if err := r.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadReview(dir)
	if err != nil || got.IID != 7 || got.HeadSHA != "abc" {
		t.Fatalf("load: %v %+v", err, got)
	}
	if got.UpdatedAt == "" {
		t.Fatal("updated_at not set")
	}
}

func TestSlugAndReviewDir(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	d := ReviewDir("group/sub/proj", 12)
	if !strings.HasSuffix(d, filepath.Join("group_sub_proj", "mr12")) {
		t.Fatalf("unexpected dir %s", d)
	}
	if s := Slug("a/../../b"); strings.ContainsAny(s, "/\\") {
		t.Fatal("slug must not contain path separators")
	}
}
