package publish

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// dispatchMR answers GET MR + user + discussions, delegates POST to the
// caller's snippet ("" becomes a no-op).
func dispatchMR(t *testing.T, dir, mrBody, postBehavior string) (string, string) {
	t.Helper()
	if postBehavior == "" {
		postBehavior = ":"
	}
	return writeGlab(t, dir, fmt.Sprintf(`
is_post=0
prev=""
for a in "$@"; do [ "$prev" = "-X" ] && is_post=1; prev="$a"; done
if [ "$is_post" = "1" ]; then
%[1]s
elif [[ "$*" == *" user"* ]]; then
  echo '{"username":"me"}'
elif [[ "$*" == *discussions* ]]; then
  echo '[]'
else
  cat <<'MREOF'
%[2]s
MREOF
fi
`, postBehavior, mrBody))
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
  echo '{"id":"disc2","notes":[{"id":22}]}'
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

func TestPublishDuplicatePrecheckNoPOST(t *testing.T) {
	dir := t.TempDir()
	bin, log := writeGlab(t, dir, fmt.Sprintf(`
if [[ "$*" == *"-X POST"* ]]; then
  echo '{"id":"new","notes":[{"id":9}]}'
elif [[ "$*" == *" user"* ]]; then
  echo '{"username":"me"}'
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
	fNew := approved("F001", "a.go", 5)
	p := positionFor(fNew)
	if p.NewLine == nil || *p.NewLine != 5 || p.LineRange != nil {
		t.Fatalf("new side: %+v", p)
	}
	fOld := approved("F002", "a.go", 5)
	fOld.Side = "old"
	p = positionFor(fOld)
	if p.OldLine == nil || *p.OldLine != 5 || p.NewLine != nil {
		t.Fatalf("old side: %+v", p)
	}
	fRange := approved("F003", "a.go", 5)
	fRange.LineEnd = 9
	p = positionFor(fRange)
	if p.LineRange == nil || *p.LineRange.Start.Line != 5 || *p.LineRange.End.Line != 9 {
		t.Fatalf("range: %+v", p)
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
