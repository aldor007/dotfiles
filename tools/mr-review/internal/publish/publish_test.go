package publish

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mr-review/internal/gitlab"
	"mr-review/internal/store"
)

const stubMR = `{"iid":7,"title":"T","author":{"username":"alice"},"state":"opened",
  "diff_refs":{"base_sha":"b","start_sha":"s","head_sha":"HEAD1"},"sha":"HEAD1"}`

// writeGlab installs a recording fake glab and returns (bin, log).
func writeGlab(t *testing.T, dir, dispatch string) (string, string) {
	t.Helper()
	log := filepath.Join(dir, "calls.log")
	bin := filepath.Join(dir, "glab")
	script := "#!/bin/bash\necho \"$*\" >> " + log + "\n" + dispatch
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

// dispatchMR answers GET MR + user + discussions + diffs listing, delegates
// POST to the caller's snippet ("" becomes a no-op). An optional diffsBody
// overrides the default empty diff listing (used by the P2 #6 rename tests).
func dispatchMR(t *testing.T, dir, mrBody, postBehavior string, diffsBody ...string) (string, string) {
	t.Helper()
	if postBehavior == "" {
		postBehavior = ":"
	}
	diffs := "[]"
	if len(diffsBody) > 0 {
		diffs = diffsBody[0]
	}
	return writeGlab(t, dir, fmt.Sprintf(`
is_post=0
prev=""
for a in "$@"; do [ "$prev" = "-X" ] && is_post=1; prev="$a"; done
if [ "$is_post" = "1" ]; then
%[1]s
elif [[ "$*" == *" user"* ]]; then
  echo '{"username":"me"}'
elif [[ "$*" == *"/diffs?"* ]]; then
  cat <<'DIFFSEOF'
%[3]s
DIFFSEOF
elif [[ "$*" == *discussions* ]]; then
  echo '[]'
else
  cat <<'MREOF'
%[2]s
MREOF
fi
`, postBehavior, mrBody, diffs))
}

func reviewDirWithFindings(t *testing.T, findings []store.Finding, head string) string {
	t.Helper()
	dir := t.TempDir()
	rev := &store.Review{
		Project: "g/p", IID: 7, Host: "", HeadSHA: head, BaseSHA: "b", StartSHA: "s",
		Title: "T", State: "reviewing",
	}
	if err := rev.Save(dir); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFindings(dir, findings); err != nil {
		t.Fatal(err)
	}
	return dir
}

func approved(id, file string, line int) store.Finding {
	return store.Finding{
		ID: id, File: file, Side: "new", Line: line, LineEnd: line,
		Severity: "P1", Kind: "bug", Title: "t-" + id,
		Explanation: "e", Evidence: "v", Comment: "comment-" + id,
		Confidence: "high", Decision: "approved",
	}
}

const postOK = `echo '{"id":"disc1","notes":[{"id":11,"body":"posted"}]}'`

func TestPublishHappyPath(t *testing.T) {
	dir := t.TempDir()
	bin, log := dispatchMR(t, dir, stubMR, postOK)
	t.Setenv("MR_REVIEW_GLAB", bin)

	rdir := reviewDirWithFindings(t,
		[]store.Finding{approved("F001", "a.go", 42), approved("F002", "b.go", 7)}, "HEAD1")

	res, err := Run(Options{Dir: rdir, Yes: true})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(res.Published) != 2 || len(res.Failed) != 0 || res.ExitCode() != 0 {
		t.Fatalf("result: %+v", res)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "position[new_line]=42") ||
		!strings.Contains(string(calls), "position[new_line]=7") {
		t.Fatalf("positions not posted:\n%s", calls)
	}
	fs, _ := store.LoadFindings(rdir)
	if fs[0].Publication == nil || fs[0].Publication.DiscussionID != "disc1" {
		t.Fatalf("publication not recorded: %+v", fs[0])
	}
	if _, err := os.Stat(filepath.Join(rdir, "publish-log.jsonl")); err != nil {
		t.Fatal("publish log missing")
	}
}

func TestPublishBlocksStaleSHA(t *testing.T) {
	dir := t.TempDir()
	bin, _ := dispatchMR(t, dir, strings.Replace(stubMR, "HEAD1", "HEAD2", -1), "")
	t.Setenv("MR_REVIEW_GLAB", bin)

	rdir := reviewDirWithFindings(t, []store.Finding{approved("F001", "a.go", 1)}, "HEAD1")
	_, err := Run(Options{Dir: rdir, Yes: true})
	if err == nil || !strings.Contains(err.Error(), "MR moved") {
		t.Fatalf("expected stale block, got %v", err)
	}
	fs, _ := store.LoadFindings(rdir)
	if fs[0].Publication != nil || fs[0].Decision != "approved" {
		t.Fatal("state must be untouched after a blocked publish")
	}
}

func TestPublishRefusesUnapproved(t *testing.T) {
	dir := t.TempDir()
	bin, _ := dispatchMR(t, dir, stubMR, "")
	t.Setenv("MR_REVIEW_GLAB", bin)

	pend := approved("F001", "a.go", 1)
	pend.Decision = "pending"
	rdir := reviewDirWithFindings(t, []store.Finding{pend}, "HEAD1")
	_, err := Run(Options{Dir: rdir, Yes: true})
	if err == nil || !strings.Contains(err.Error(), "nothing to publish") {
		t.Fatalf("expected refusal, got %v", err)
	}
}

func TestPublishClosedMRRefused(t *testing.T) {
	dir := t.TempDir()
	bin, _ := dispatchMR(t, dir, strings.Replace(stubMR, `"opened"`, `"merged"`, -1), "")
	t.Setenv("MR_REVIEW_GLAB", bin)

	rdir := reviewDirWithFindings(t, []store.Finding{approved("F001", "a.go", 1)}, "HEAD1")
	_, err := Run(Options{Dir: rdir, Yes: true})
	if err == nil || !strings.Contains(err.Error(), "merged") {
		t.Fatalf("expected closed-MR refusal, got %v", err)
	}
}

func TestPublishPartialFailureAndSafeRetry(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(dir, "failed_once")
	post := fmt.Sprintf(`
if [ -f %[1]s ]; then
  echo '{"id":"disc2","notes":[{"id":22,"position":{"new_line":1}}]}'
else
  touch %[1]s
  echo "400 invalid position" >&2
  exit 1
fi
`, flag)
	bin, log := dispatchMR(t, dir, stubMR, post)
	t.Setenv("MR_REVIEW_GLAB", bin)

	rdir := reviewDirWithFindings(t,
		[]store.Finding{approved("F001", "a.go", 1), approved("F002", "a.go", 2)}, "HEAD1")

	res, err := Run(Options{Dir: rdir, Yes: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode() != 3 || len(res.Published) != 1 || len(res.Failed) != 1 {
		t.Fatalf("honest partial result expected: %+v", res)
	}

	// retry: transient problem is gone (flag persists = it failed once);
	// the failed one posts now, the published one is skipped (idempotent)
	res2, err := Run(Options{Dir: rdir, Yes: true})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(res2.Published) != 1 || len(res2.Failed) != 0 {
		t.Fatalf("retry must publish exactly the failed one: %+v", res2)
	}
	calls, _ := os.ReadFile(log)
	if strings.Count(string(calls), "comment-F002") != 1 {
		t.Fatalf("already-published finding must never POST again:\n%s", calls)
	}
}

// TestPublishDroppedPositionFallsBack pins the GitLab behavior observed on
// real MRs: the POST is accepted (201) but the note comes back WITHOUT a
// position — GitLab silently converted the inline comment into a general
// note. The publisher must detect that from the POST response, PUT-edit the
// note to prepend the file:line location, and report the finding as
// published-but-unanchored (never silently claim inline anchoring).
func TestPublishDroppedPositionFallsBack(t *testing.T) {
	dir := t.TempDir()
	post := `
if [[ "$*" == *"-X POST"* ]]; then
  echo '{"id":"discG","notes":[{"id":33}]}'
elif [[ "$*" == *" user"* ]]; then
  echo '{"username":"me"}'
elif [[ "$*" == *"/diffs?"* ]]; then
  echo '[]'
else
  echo '[]'
fi
`
	bin, log := dispatchMR(t, dir, stubMR, post)
	t.Setenv("MR_REVIEW_GLAB", bin)

	rdir := reviewDirWithFindings(t, []store.Finding{approved("F009", "a.go", 7)}, "HEAD1")
	res, err := Run(Options{Dir: rdir, Yes: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Published) != 1 || len(res.Unanchored) != 1 || len(res.Failed) != 0 {
		t.Fatalf("expected published+unanchored, got: %+v", res)
	}
	if !strings.Contains(res.Unanchored[0], "F009: a.go:7") {
		t.Fatalf("unanchored entry must carry file:line: %q", res.Unanchored[0])
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "-X PUT") || !strings.Contains(string(calls), "**`a.go:7`**") {
		t.Fatalf("expected PUT location-header edit, calls:\n%s", calls)
	}
	// the publication is still recorded so retries skip, never re-POST
	if strings.Count(string(calls), "-X POST") != 1 {
		t.Fatalf("exactly one POST expected:\n%s", calls)
	}
}

func TestPublishDuplicatePrecheckNoPOST(t *testing.T) {
	dir := t.TempDir()
	bin, log := writeGlab(t, dir, fmt.Sprintf(`
if [[ "$*" == *"-X POST"* ]]; then
  echo '{"id":"new","notes":[{"id":9}]}'
elif [[ "$*" == *" user"* ]]; then
  echo '{"username":"me"}'
elif [[ "$*" == *"/diffs?"* ]]; then
  echo '[]'
elif [[ "$*" == *"discussions"* ]]; then
  cat <<'EOF'
[{"id":"d1","notes":[{"id":3,"author":{"username":"me"},"body":"comment-F001","position":{"new_path":"a.go","new_line":1,"old_path":"a.go"}}]}]
EOF
else
  cat <<'MREOF'
%[1]s
MREOF
fi
`, stubMR))
	t.Setenv("MR_REVIEW_GLAB", bin)

	rdir := reviewDirWithFindings(t, []store.Finding{approved("F001", "a.go", 1)}, "HEAD1")
	res, err := Run(Options{Dir: rdir, Yes: true})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Published) != 0 || len(res.Skipped) == 0 {
		t.Fatalf("duplicate must be skipped: %+v", res)
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), "-X POST") {
		t.Fatalf("no POST should happen for duplicates:\n%s", calls)
	}
}

func TestConfirmRequiresExplicitYes(t *testing.T) {
	if err := confirm([]store.Finding{approved("F001", "a.go", 1)}, nil, ""); err == nil {
		t.Fatal("non-TTY stdin must refuse without --yes")
	}
}

func TestPositionForSides(t *testing.T) {
	paths := diffPathsOf([]gitlab.MRDiff{
		{OldPath: "a.go", NewPath: "a.go"},
		{OldPath: "old_name.go", NewPath: "new_name.go", RenamedFile: true},
	})
	fNew := approved("F001", "a.go", 5)
	p := positionFor(fNew, paths, nil)
	if p.NewLine == nil || *p.NewLine != 5 || p.LineRange != nil {
		t.Fatalf("new side: %+v", p)
	}
	fOld := approved("F002", "a.go", 5)
	fOld.Side = "old"
	p = positionFor(fOld, paths, nil)
	if p.OldLine == nil || *p.OldLine != 5 || p.NewLine != nil {
		t.Fatalf("old side: %+v", p)
	}
	// P2 #6: a renamed file's position carries the diff entry's exact pair
	fRen := approved("F004", "new_name.go", 2)
	p = positionFor(fRen, paths, nil)
	if p.NewPath != "new_name.go" || p.OldPath != "old_name.go" {
		t.Fatalf("rename paths: %+v", p)
	}
	// a path absent from the diff falls back to itself on both sides
	p = positionFor(approved("F005", "other.go", 1), paths, nil)
	if p.NewPath != "other.go" || p.OldPath != "other.go" {
		t.Fatalf("unknown-path fallback: %+v", p)
	}
	fRange := approved("F003", "a.go", 5)
	fRange.LineEnd = 9
	p = positionFor(fRange, paths, nil)
	if p.LineRange == nil || *p.LineRange.Start.Line != 5 || *p.LineRange.End.Line != 9 {
		t.Fatalf("range: %+v", p)
	}
}

// ---- P2 #6: inline positions must carry the diff entry's exact paths ------

// renamedDiffs: one rename (old_name.go → new_name.go) and one deletion.
const renamedDiffs = `[
  {"old_path":"old_name.go","new_path":"new_name.go","renamed_file":true,
   "diff":"@@ -1,3 +1,3 @@\n-a\n+b\n c\n"},
  {"old_path":"gone.go","new_path":"gone.go","deleted_file":true,
   "diff":"@@ -1 +0,0 @@\n-x\n"}
]`

func callsFile(t *testing.T, log string) []byte {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPublishRenamedFilePositionUsesExactPaths(t *testing.T) {
	dir := t.TempDir()
	bin, log := dispatchMR(t, dir, stubMR, postOK, renamedDiffs)
	t.Setenv("MR_REVIEW_GLAB", bin)

	rdir := reviewDirWithFindings(t, []store.Finding{approved("F001", "new_name.go", 3)}, "HEAD1")
	if _, err := Run(Options{Dir: rdir, Yes: true}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{
		"position[new_path]=new_name.go",
		"position[old_path]=old_name.go",
		"position[new_line]=3",
	} {
		if !strings.Contains(string(calls), want) {
			t.Fatalf("missing %q:\n%s", want, calls)
		}
	}
}

func TestPublishRenamedFileLineRangePosition(t *testing.T) {
	dir := t.TempDir()
	bin, log := dispatchMR(t, dir, stubMR, postOK, renamedDiffs)
	t.Setenv("MR_REVIEW_GLAB", bin)

	f := approved("F001", "new_name.go", 5)
	f.LineEnd = 9
	rdir := reviewDirWithFindings(t, []store.Finding{f}, "HEAD1")
	if _, err := Run(Options{Dir: rdir, Yes: true}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{
		"position[new_path]=new_name.go",
		"position[old_path]=old_name.go",
		"position[line_range][start][line]=5",
		"position[line_range][end][line]=9",
	} {
		if !strings.Contains(string(calls), want) {
			t.Fatalf("missing %q:\n%s", want, calls)
		}
	}
}

func TestPublishDeletedLinePosition(t *testing.T) {
	dir := t.TempDir()
	bin, log := dispatchMR(t, dir, stubMR, postOK, renamedDiffs)
	t.Setenv("MR_REVIEW_GLAB", bin)

	f := approved("F001", "gone.go", 1)
	f.Side = "old"
	rdir := reviewDirWithFindings(t, []store.Finding{f}, "HEAD1")
	if _, err := Run(Options{Dir: rdir, Yes: true}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	s := string(callsFile(t, log))
	if !strings.Contains(s, "position[old_line]=1") || strings.Contains(s, "position[new_line]=") {
		t.Fatalf("old-side position wrong:\n%s", s)
	}
	if !strings.Contains(s, "position[old_path]=gone.go") || !strings.Contains(s, "position[new_path]=gone.go") {
		t.Fatalf("deleted-file paths wrong:\n%s", s)
	}
}

func TestPublishDraftCommentRenamedFilePosition(t *testing.T) {
	dir := t.TempDir()
	bin, log := dispatchMR(t, dir, stubMR, postOK, renamedDiffs)
	t.Setenv("MR_REVIEW_GLAB", bin)

	rdir := reviewDirWithFindings(t, nil, "HEAD1")
	draft := store.DraftComment{
		ID: "C001", File: "new_name.go", Side: "new", Line: 3, LineEnd: 3,
		Text: "draft on a renamed file", Origin: "human", State: store.CommentReady,
	}
	if err := store.SaveComments(rdir, []store.DraftComment{draft}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{Dir: rdir, Yes: true}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	calls, _ := os.ReadFile(log)
	for _, want := range []string{
		"position[new_path]=new_name.go",
		"position[old_path]=old_name.go",
		"position[new_line]=3",
		"body=draft on a renamed file",
	} {
		if !strings.Contains(string(calls), want) {
			t.Fatalf("missing %q:\n%s", want, calls)
		}
	}
}

// ---- P2 #7: the summary note is published exactly once ---------------------

func TestPublishSummaryIdempotent(t *testing.T) {
	dir := t.TempDir()
	bin, log := dispatchMR(t, dir, stubMR, postOK)
	t.Setenv("MR_REVIEW_GLAB", bin)

	rdir := t.TempDir()
	rev := &store.Review{
		Project: "g/p", IID: 7, Host: "", HeadSHA: "HEAD1", BaseSHA: "b", StartSHA: "s",
		Title: "T", State: "reviewing", SummaryComment: "REVIEW SUMMARY BODY",
	}
	if err := rev.Save(rdir); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFindings(rdir, nil); err != nil {
		t.Fatal(err)
	}

	res, err := Run(Options{Dir: rdir, Yes: true, Summary: true})
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if !res.Summary {
		t.Fatalf("summary must post on the first run: %+v", res)
	}
	saved, err := store.LoadReview(rdir)
	if err != nil || saved.SummaryDiscussionID != "disc1" {
		t.Fatalf("summary discussion id not persisted: %+v %v", saved, err)
	}

	// second run of the SAME completed review: no duplicate summary
	res2, err := Run(Options{Dir: rdir, Yes: true, Summary: true})
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if res2.Summary {
		t.Fatalf("summary must not post twice: %+v", res2)
	}
	found := false
	for _, s := range res2.Skipped {
		if strings.Contains(s, "summary") && strings.Contains(s, "already published") {
			found = true
		}
	}
	if !found {
		t.Fatalf("second run must report the skip: %+v", res2)
	}
	calls, _ := os.ReadFile(log)
	if n := strings.Count(string(calls), "REVIEW SUMMARY BODY"); n != 1 {
		t.Fatalf("summary posted %d times, want exactly 1:\n%s", n, calls)
	}
}

func TestDryRunPostsNothing(t *testing.T) {
	dir := t.TempDir()
	bin, log := dispatchMR(t, dir, stubMR, postOK)
	t.Setenv("MR_REVIEW_GLAB", bin)

	rdir := reviewDirWithFindings(t, []store.Finding{approved("F001", "a.go", 1)}, "HEAD1")
	res, err := Run(Options{Dir: rdir, Yes: true, DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), "-X POST") {
		t.Fatal("dry run must not POST")
	}
	if len(res.Skipped) == 0 {
		t.Fatal("dry run must mark everything skipped")
	}
	fs, _ := store.LoadFindings(rdir)
	if fs[0].Publication != nil {
		t.Fatal("dry run must not record publications")
	}
}
