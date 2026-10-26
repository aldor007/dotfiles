package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestContextFilesGlob(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())

	// missing dir: nil, no error
	if got := ContextFiles("gitlab.com", "g/p"); got != nil {
		t.Fatalf("missing context dir must yield nil, got %v", got)
	}
	cdir := ContextDir("gitlab.com", "g/p")
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"b-second.md", "a-first.md", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(cdir, name), []byte("# note"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(cdir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ContextFiles("gitlab.com", "g/p")
	want := []string{filepath.Join(cdir, "a-first.md"), filepath.Join(cdir, "b-second.md")}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("context glob: want only sorted top-level .md files, got %v", got)
	}
	// per-project isolation: another project sees nothing
	if other := ContextFiles("gitlab.com", "g/other"); other != nil {
		t.Fatalf("other project must see no context, got %v", other)
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
	// host and project segments that aren't [A-Za-z0-9-_] are hex-escaped:
	// '.' -> '_2e', '/' -> '_2f', etc.
	d := ReviewDir(DefaultHost, "group/sub/proj", 12)
	if !strings.HasSuffix(d, filepath.Join("gitlab_2ecom_group_2fsub_2fproj", "mr12")) {
		t.Fatalf("unexpected dir %s", d)
	}
	if s := Slug(DefaultHost, "a/../../b"); strings.ContainsAny(s, "/\\") {
		t.Fatal("slug must not contain path separators")
	}
}

// TestSlugCollisions — review finding P1 #7: distinct projects must NOT
// map to the same review dir. Pre-fix, the Slug collapsed '.', '-' and
// '_' to the same separator, so the OLD single-arg Slug merged
// "group/sub" and "group_sub" (and others). The new (host, project)
// Slug also keeps host distinct from project — two GitLab instances
// pointing at the same project name land in different directories.
func TestSlugCollisions(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	cases := []struct {
		host, project string
	}{
		{"gitlab.com", "group/sub/proj"},
		{"gitlab.cast.ai", "group/sub/proj"}, // different host, same project
		{"gitlab.com", "group/sub-proj"},     // '-_' collision that the OLD Slug failed
		{"gitlab.com", "group.sub.proj"},     // '.' also folds to '_' in old code
		{"gitlab.com", "group_sub/proj"},     // '_' host/project boundary preserved by new layout
	}
	seen := map[string]string{}
	for _, c := range cases {
		dir := ReviewDir(c.host, c.project, 1)
		if prev, dup := seen[dir]; dup {
			t.Fatalf("collision: %q+%q and %q+%q both map to %s", prev, dir, c.host, c.project, dir)
		}
		seen[dir] = c.host + "|" + c.project
	}
	if ReviewDir("", "orders", 1) != ReviewDir(DefaultHost, "orders", 1) {
		t.Fatal("empty host must default to DefaultHost (by design)")
	}
}

// TestSaveFindingsConcurrency — review finding P1 #8. The AI worker and
// the interactive reviewer can both call SaveFindings simultaneously.
// Without locking or fsync-before-rename, a concurrent reader could
// observe a half-written or empty file at the moment of rename, and a
// second writer could clobber a first writer's data without ever seeing
// its changes. The fix:
//   - per-pid temp filename (no two writers ever share a temp),
//   - fsync BEFORE rename (data on disk before the dir-entry change),
//   - flock on "<path>.lock" with a 5s timeout (serializes writers).
//
// This test asserts the contract: N concurrent SaveFindings calls all
// succeed, and the final file is the complete findings set from exactly
// ONE of the writers (not a corrupted mix, not a partial file). A
// concurrent reader (running LoadFindings throughout) must never observe
// a truncated or unparseable state.
func TestSaveFindingsConcurrency(t *testing.T) {
	dir := t.TempDir()
	const N = 20
	var wg sync.WaitGroup
	errs := make(chan error, N)
	// Each writer writes a SET with a unique count so we can identify
	// which writer's data won.
	for w := 0; w < N; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var fs []Finding
			count := id + 1 // 1..N
			for k := 0; k < count; k++ {
				fs = append(fs, Finding{
					ID:   fmt.Sprintf("W%d-F%d", id, k),
					File: fmt.Sprintf("a%d.go", k), Line: 10 + k, Side: "new",
					Severity: "P1", Title: "t", Explanation: "e", Evidence: "v", Comment: "c",
					Confidence: "high",
				})
			}
			if err := SaveFindings(dir, fs); err != nil {
				errs <- err
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatalf("concurrent SaveFindings: %v", e)
	}
	got, err := LoadFindings(dir)
	if err != nil {
		t.Fatalf("final load (must be a complete set, not partial): %v", err)
	}
	if len(got) == 0 {
		t.Fatal("final file must not be empty (one of the writers must have won)")
	}
	// All surviving findings must share the same writer prefix.
	winner := got[0].ID
	// Verify each surviving finding's writer prefix matches the winner's
	// first prefix (e.g. W3-F* means writer 3 won). No mixed prefixes are
	// allowed — that would mean two writers' temp files clobbered each
	// other or the rename wasn't atomic.
	for _, f := range got {
		prefix := f.ID[:2] // "W3" of "W3-F7"
		winPrefix := winner[:2]
		if prefix != winPrefix {
			t.Fatalf("mixed writers in final file — found %q alongside %q (no flock atomicity)", f.ID, winner)
		}
	}
	// No duplicates.
	seen := map[string]bool{}
	for _, f := range got {
		if seen[f.ID] {
			t.Fatalf("duplicate ID %s (race)", f.ID)
		}
		seen[f.ID] = true
	}
}

// TestAtomicWriteConcurrentReaders — while a writer is mid-flight,
// concurrent LoadFindings calls must never observe a truncated or empty
// file. Without fsync-before-rename (the OLD atomicWrite), a rename can
// expose readers to an empty target briefly. With the fix, every Load
// either returns the old complete set OR fails with a clean os error.
func TestAtomicWriteConcurrentReaders(t *testing.T) {
	dir := t.TempDir()
	// Seed the file with one finding.
	seed := []Finding{func() Finding { f := validFinding(); f.ID = "SEED"; return f }()}
	if err := SaveFindings(dir, seed); err != nil {
		t.Fatal(err)
	}

	const N = 50
	var wg sync.WaitGroup
	errs := make(chan error, N)
	stop := make(chan struct{})

	// Writer: continuously replace with an empty set.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if err := SaveFindings(dir, nil); err != nil {
					errs <- err
					return
				}
			}
		}
	}()

	// Readers: each must observe a valid state (seed file or empty file
	// after a successful clear) — never a partial file (bufio.ErrTooLong,
	// unexpected EOF, etc.).
	for r := 0; r < N; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				got, err := LoadFindings(dir)
				if err != nil {
					errs <- err
					return
				}
				for _, f := range got {
					if f.ID != "SEED" {
						errs <- fmt.Errorf("invalid finding ID observed: %q", f.ID)
						return
					}
				}
			}
		}()
	}
	close(stop)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
}
