package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDraftCommentValidate(t *testing.T) {
	cases := []struct {
		name    string
		c       DraftComment
		wantErr string
	}{
		{"ok inline", DraftComment{ID: "C001", File: "a/b.go", Line: 5, Text: "x"}, ""},
		{"ok range", DraftComment{ID: "C001", File: "a/b.go", Line: 5, LineEnd: 9, Text: "x"}, ""},
		{"ok general", DraftComment{ID: "C001", General: true, Text: "x"}, ""},
		{"missing id", DraftComment{File: "a.go", Line: 1, Text: "x"}, "missing id"},
		{"missing text", DraftComment{ID: "C001"}, "missing text"},
		{"missing file", DraftComment{ID: "C001", Text: "x"}, "missing file"},
		{"abs path", DraftComment{ID: "C001", File: "/etc/passwd", Line: 1, Text: "x"}, "unsafe"},
		{"traversal", DraftComment{ID: "C001", File: "../x.go", Line: 1, Text: "x"}, "unsafe"},
		{"bad line", DraftComment{ID: "C001", File: "a.go", Line: 0, Text: "x"}, "bad line"},
		{"bad side", DraftComment{ID: "C001", File: "a.go", Side: "middle", Line: 1, Text: "x"}, "bad side"},
		{"bad state", DraftComment{ID: "C001", File: "a.go", Line: 1, Text: "x", State: "nope"}, "bad state"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestDraftCommentNormalize(t *testing.T) {
	c := DraftComment{ID: "C001", File: "a.go", Line: 7, LineEnd: 3, Text: "x"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.LineEnd != 7 {
		t.Fatalf("line_end must clamp to line, got %d", c.LineEnd)
	}
	if c.Side != "new" || c.State != CommentReady || c.Origin != "human" {
		t.Fatalf("defaults wrong: side=%q state=%q origin=%q", c.Side, c.State, c.Origin)
	}
}

func TestCommentsStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cs, err := LoadComments(dir)
	if err != nil || len(cs) != 0 {
		t.Fatalf("empty load: %v %d", err, len(cs))
	}
	in := []DraftComment{
		NewDraftComment("C001", "services/x.go", "new", 12, 12, false, "first"),
		NewDraftComment("C002", "services/x.go", "old", 3, 0, false, "old side"),
		NewDraftComment("C003", "", "", 0, 0, true, "general note"),
	}
	in[2].State = CommentDraft
	if err := SaveComments(dir, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadComments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 comments, got %d", len(out))
	}
	if out[0].ID != "C001" || out[0].Line != 12 || out[0].Origin != "human" || out[0].State != CommentReady {
		t.Fatalf("comment 0 mismatch: %+v", out[0])
	}
	if out[1].Side != "old" || out[1].LineEnd != 3 {
		t.Fatalf("comment 1 mismatch: %+v", out[1])
	}
	if !out[2].General || out[2].State != CommentDraft {
		t.Fatalf("comment 2 mismatch: %+v", out[2])
	}
	// corrupt line fails closed
	if err := os.WriteFile(CommentsPath(dir), []byte("{bad json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadComments(dir); err == nil {
		t.Fatal("corrupt comments.jsonl must fail closed")
	}
}

func TestNextCommentID(t *testing.T) {
	got := NextCommentID(nil)
	if got != "C001" {
		t.Fatalf("first id = %q", got)
	}
	got = NextCommentID([]DraftComment{{ID: "C004"}})
	if got != "C005" {
		t.Fatalf("after C004 = %q", got)
	}
}

func TestReviewedFilesPersist(t *testing.T) {
	dir := t.TempDir()
	rev := &Review{Project: "p", IID: 1, HeadSHA: "abc", CreatedAt: "t"}
	rev.ReviewedFiles = []string{"a.go", "b/c.go"}
	if err := rev.Save(dir); err != nil {
		t.Fatal(err)
	}
	rev2, err := LoadReview(filepath.Join(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(rev2.ReviewedFiles) != 2 || rev2.ReviewedFiles[1] != "b/c.go" {
		t.Fatalf("reviewed files lost: %+v", rev2.ReviewedFiles)
	}
}
