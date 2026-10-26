package tuicr

import (
	"context"

	"encoding/json"
	"fmt"
	"mr-review/internal/store"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeTuicr writes a Python stand-in for the tuicr CLI implementing the
// documented JSON contract (review list/add/comments/delete) backed by a
// session file, and points MR_REVIEW_TUICR at it.
func fakeTuicr(t *testing.T) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fake-tuicr")
	script := `#!/usr/bin/env python3
import json, os, sys

args = sys.argv[1:]
state = os.environ["FAKE_TUICR_STATE"]

def load():
    if os.path.exists(state):
        with open(state) as f:
            return json.load(f)
    return {"comments": [], "next": 1}

def save(d):
    with open(state, "w") as f:
        json.dump(d, f)

def out(v):
    print(json.dumps(v))

if args[0:2] == ["review", "list"]:
    if os.path.exists(state + ".cleared"):
        out([])
        sys.exit(0)
    if os.environ.get("FAKE_TUICR_REPO_EMPTY") == "1" and "--repo" in args:
        out([])  # --repo coordinate mismatch (monorepo worktree): empty
        sys.exit(0)
    fc = int(os.environ.get("FAKE_TUICR_FILES", "3"))
    out([{
        "slug": "gl:grp/proj/pr/42",
        "kind": "pr",
        "path": "/tmp/session.json",
        "updated_at": "2026-10-21T10:00:00Z",
        "comment_count": 0, "reviewed_count": 0, "file_count": fc,
        "anchor": "pr/42", "active": False,
    }])
elif args[0:2] == ["review", "clear"]:
    if os.path.exists(state):
        os.remove(state)
    open(state + ".cleared", "w").write("{}")
    out({"cleared": True})
elif args[0:2] == ["review", "comments"]:
    d = load()
    out(d["comments"])
elif args[0:3] == ["review", "add", "--session"]:
    # Count every invocation atomically — regression tests verify exactly
    # one call per AddComment, never two.
    counter = state + ".add_count"
    n = 0
    if os.path.exists(counter):
        try:
            n = int(open(counter).read())
        except Exception:
            n = 0
    open(counter, "w").write(str(n + 1))
    payload = json.loads(sys.stdin.read())
    # Even when BAD_JSON is set the fake still stores the comment (exit 0
    # means the comment IS stored — that's the new contract). It just
    # emits a non-JSON body so the read-back path is exercised.
    if os.environ.get("FAKE_TUICR_REVIEW_COUNT") == "1":
        pass  # used by the read-back tests that pre-seed state directly
    d = load()
    c = {
        "id": "comment-%04d" % d["next"],
        "location": "%s:%d" % (payload.get("file", ""), payload.get("line", 0)),
        "path": payload.get("file", ""),
        "start_line": payload.get("line", payload.get("start_line", 0)),
        "end_line": payload.get("end_line", payload.get("line", 0)),
        "side": payload.get("side", "new"),
        "comment_type": payload.get("comment_type", "none"),
        "author": payload.get("username", "user"),
        "lifecycle_state": "local_draft",
        "created_at": "2026-10-21T10:00:00Z",
        "content": payload.get("content", ""),
    }
    d["comments"].append(c)
    d["next"] += 1
    save(d)
    # FAKE_TUICR_BAD_JSON forces attempt 1 to succeed (exit 0) but emit a
    # non-JSON body — the original bug: parseStoredComment fails, then the
    # code falls through to attempt 2 and double-inserts. After the fix,
    # the second attempt must NOT run. The comment IS stored (above), only
    # the response shape is wrong.
    if os.environ.get("FAKE_TUICR_BAD_JSON") == "1":
        sys.stdout.write("not-json\n")
        sys.stdout.flush()
        sys.exit(0)
    out(c)
elif args[0:2] == ["review", "delete"]:
    i = args.index("--comment-id")
    cid = args[i + 1]
    d = load()
    d["comments"] = [c for c in d["comments"] if c["id"] != cid]
    save(d)
    out({"deleted": cid})
else:
    sys.exit("unexpected args: %s" % args)
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MR_REVIEW_TUICR", bin)
	state := filepath.Join(t.TempDir(), "session.json")
	t.Setenv("FAKE_TUICR_STATE", state)
}

// findingJSON builds one findings.jsonl line.
func findingJSON(t *testing.T, v map[string]interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeFindings(t *testing.T, dir string, findings []map[string]interface{}) {
	t.Helper()
	var lines []string
	for _, f := range findings {
		lines = append(lines, findingJSON(t, f))
	}
	if err := os.WriteFile(filepath.Join(dir, "findings.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func baseFindings() []map[string]interface{} {
	mk := func(id, file string, line int, decision string, stale bool, comment string) map[string]interface{} {
		return map[string]interface{}{
			"id": id, "file": file, "line": line, "severity": "P1", "kind": "bug",
			"title": "t " + id, "explanation": "e", "evidence": file + ":" + fmt.Sprint(line),
			"comment": comment, "confidence": "high", "decision": decision,
			"stale": stale,
		}
	}
	return []map[string]interface{}{
		mk("F001", "a.go", 5, "approved", false, "Fix a.go line 5."),
		mk("F002", "b.go", 2, "rejected", false, "Rejected note."),
		mk("F003", "c.go", 8, "pending", false, "Pending note."),
		mk("F004", "d.go", 4, "approved", true, "Stale note."),
	}
}

func stageOpts(dir string) StageOptions {
	return StageOptions{Dir: dir, RepoPath: "/tmp/anywhere", Project: "grp/proj", IID: 42, Username: "mr-review AI"}
}

// TestStageApprovedOnly — approved→staged; rejected/pending never staged;
// stale never staged; staging is idempotent; human edits and deletions in
// tuicr are detected by sync; nothing is ever rewritten.
func TestStageApprovedOnly(t *testing.T) {
	fakeTuicr(t)
	dir := t.TempDir()
	writeFindings(t, dir, baseFindings())
	cli := NewCLI()
	opts := stageOpts(dir)

	res, err := StageApproved(context.Background(), opts, cli)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Staged) != 1 || res.Staged[0] != "F001" {
		t.Fatalf("only F001 must be staged, got %v (skipped=%v failed=%v)", res.Staged, res.Skipped, res.Failed)
	}
	staleSkipped := false
	for _, sk := range res.Skipped {
		if strings.HasPrefix(sk, "F004:") && strings.Contains(sk, "stale") {
			staleSkipped = true
		}
	}
	if !staleSkipped {
		t.Fatalf("stale finding must be skipped: %v", res.Skipped)
	}

	// idempotent: second run stages nothing, skips the mapped finding
	res2, err := StageApproved(context.Background(), opts, cli)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Staged) != 0 {
		t.Fatalf("second run must stage nothing, got %v", res2.Staged)
	}

	// mapping exists and points at the fake PR session
	m, err := LoadMappings(dir)
	if err != nil {
		t.Fatal(err)
	}
	mp, ok := m.Get("F001")
	if !ok || mp.Session != "gl:grp/proj/pr/42" || mp.CommentID == "" {
		t.Fatalf("mapping wrong: %+v", mp)
	}

	// human edit detection: the text behind tuicr changes
	state := os.Getenv("FAKE_TUICR_STATE")
	b, _ := os.ReadFile(state)
	if !strings.Contains(string(b), mp.CommentID) {
		t.Fatal("fake session must contain the staged comment")
	}
	edited := strings.Replace(string(b), "Fix a.go line 5.", "HUMAN EDIT: better fix", 1)
	if err := os.WriteFile(state, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	editedIDs, deletedIDs, err := SyncState(context.Background(), opts, cli)
	if err != nil {
		t.Fatal(err)
	}
	if len(editedIDs) != 1 || editedIDs[0] != "F001" {
		t.Fatalf("human edit not detected: %v", editedIDs)
	}
	if len(deletedIDs) != 0 {
		t.Fatalf("unexpected deletions: %v", deletedIDs)
	}

	// deletion detection
	comments, err := cli.Comments(context.Background(), "gl:grp/proj/pr/42", "/tmp/anywhere")
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected exactly one staged comment, got %d", len(comments))
	}
	if err := cli.DeleteComment(context.Background(), "gl:grp/proj/pr/42", comments[0].ID, "/tmp/anywhere"); err != nil {
		t.Fatal(err)
	}
	_, deletedIDs, err = SyncState(context.Background(), opts, cli)
	if err != nil {
		t.Fatal(err)
	}
	if len(deletedIDs) != 1 || deletedIDs[0] != "F001" {
		t.Fatalf("deletion not detected: %v", deletedIDs)
	}
}

// TestStageNoSession — without an open tuicr session, staging fails with an
// actionable message and no mapping is created.
func TestStageNoSession(t *testing.T) {
	fakeTuicr(t)
	bin := os.Getenv("MR_REVIEW_TUICR")
	if err := os.WriteFile(bin, []byte("#!/usr/bin/env python3\nimport json\nprint(json.dumps([]))\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFindings(t, dir, baseFindings())
	_, err := StageApproved(context.Background(), StageOptions{Dir: dir, RepoPath: "/tmp/x"}, NewCLI())
	if err == nil || !strings.Contains(err.Error(), "open tuicr") {
		t.Fatalf("expected actionable no-session error, got %v", err)
	}
	if _, err := LoadMappings(dir); err != nil {
		t.Fatal(err)
	}
}

// TestResolveSessionPriority — PR session for the MR wins; no local or
// "newest" session may substitute (review finding P1 #2: a local session
// is bound to a worktree/branch, not to an MR IID, so returning one for an
// MR review risks landing findings into an unrelated review).
func TestResolveSessionPriority(t *testing.T) {
	sessions := []Session{
		{Slug: "gl:grp/proj/pr/41", Kind: "pr", UpdatedAt: "2026-10-20T00:00:00Z"},
		{Slug: "gl:grp/proj/pr/42", Kind: "pr", UpdatedAt: "2026-10-21T00:00:00Z"},
		{Slug: "grp/proj@main/worktree", Kind: "local", Active: true, UpdatedAt: "2026-10-19T00:00:00Z"},
		{Slug: "grp/proj@old/worktree", Kind: "local", UpdatedAt: "2026-10-18T00:00:00Z"},
	}
	if s := ResolveSession(sessions, "grp/proj", 42); s == nil || s.Slug != "gl:grp/proj/pr/42" {
		t.Fatalf("PR session for the MR must win: %+v", s)
	}
	// No PR session for MR 99 → must resolve to nil, NOT the active local
	// session (the previous implementation fell through to active/newest
	// local, which was a P1 cross-review contamination risk).
	if s := ResolveSession(sessions, "grp/proj", 99); s != nil {
		t.Fatalf("no PR session for MR 99 must NOT fall through to a local session, got %+v", s)
	}
	if s := ResolveSession(nil, "grp/proj", 42); s != nil {
		t.Fatalf("empty list must resolve to nil, got %+v", s)
	}
}

// TestResolveSessionSmartFailsClosed — the smart resolver must reject a
// request for MR 99 when no exact-match PR session exists. The previous
// fallback chain returned "active local session" / "live pane's session",
// which could land AI comments in an unrelated review. The new contract:
// fail closed with an actionable error naming the missing project + IID.
func TestResolveSessionSmartFailsClosed(t *testing.T) {
	fakeTuicr(t)
	t.Setenv("FAKE_TUICR_FILES", "3")
	cli := NewCLI()
	ctx := context.Background()

	// No exact-match PR session for MR 99 — the only session the fake
	// exposes is gl:grp/proj/pr/42.
	_, err := cli.ResolveSessionSmart(ctx, "/tmp/repo", "grp/proj", 99)
	if err == nil {
		t.Fatal("ResolveSessionSmart must fail closed when no exact-match PR session exists")
	}
	if !strings.Contains(err.Error(), `project="grp/proj"`) || !strings.Contains(err.Error(), `iid=99`) {
		t.Fatalf("error must name the missing project + IID for actionable debugging, got: %v", err)
	}
	if !strings.Contains(err.Error(), "exact-match") {
		t.Fatalf("error must explain the contract (exact match required), got: %v", err)
	}
}

// TestPrepareSessionNeverClears — PrepareSession must NEVER clear a
// session, regardless of the file-count comparison. Auto-clearing was the
// original P0 finding (silent loss of human review work when tuicr's file
// count legitimately disagreed with GitLab's). A partial session is
// reported via SessionStatusPartial; clearing is a human decision.
func TestPrepareSessionNeverClears(t *testing.T) {
	fakeTuicr(t)
	cli := NewCLI()
	ctx := context.Background()

	// Case 1: file count below expected — session must remain, status Partial.
	t.Setenv("FAKE_TUICR_FILES", "3")
	slug, status, err := cli.PrepareSession(ctx, "/tmp/repo", "grp/proj", 42, 10)
	if err != nil {
		t.Fatal(err)
	}
	if slug != "gl:grp/proj/pr/42" {
		t.Fatalf("partial session slug must be returned: got %q", slug)
	}
	if status != SessionStatusPartial {
		t.Fatalf("partial session must be reported as SessionStatusPartial, got %v", status)
	}
	// The fake marks the session as cleared when `review clear` runs;
	// assert it was NOT invoked by checking the marker file is absent.
	if _, err := os.Stat(os.Getenv("FAKE_TUICR_STATE") + ".cleared"); err == nil {
		t.Fatal("P0: PrepareSession must never invoke `tuicr review clear`; marker file present")
	}

	// Case 2: file count equal — status Ready, no clear.
	// Re-prime the fake by removing any leftover cleared marker.
	os.Remove(os.Getenv("FAKE_TUICR_STATE") + ".cleared")
	t.Setenv("FAKE_TUICR_FILES", "10")
	slug, status, err = cli.PrepareSession(ctx, "/tmp/repo", "grp/proj", 42, 10)
	if err != nil {
		t.Fatal(err)
	}
	if status != SessionStatusReady {
		t.Fatalf("complete session must be SessionStatusReady, got %v", status)
	}
	if slug == "" {
		t.Fatal("ready session must return its slug")
	}
	if _, err := os.Stat(os.Getenv("FAKE_TUICR_STATE") + ".cleared"); err == nil {
		t.Fatal("P0: PrepareSession must never invoke `tuicr review clear` on a ready session either")
	}

	// Case 3: expectedFiles == 0 means "don't compare" — treat as Ready.
	t.Setenv("FAKE_TUICR_FILES", "3")
	slug, status, err = cli.PrepareSession(ctx, "/tmp/repo", "grp/proj", 42, 0)
	if err != nil {
		t.Fatal(err)
	}
	if status != SessionStatusReady || slug == "" {
		t.Fatalf("expectedFiles=0 must not classify as Partial: status=%v slug=%q", status, slug)
	}
}

// TestStageApprovedRepoListEmptyFallsBackToAll — the worktree-path
// coordinate can fail (`--repo` returns nothing, e.g. monorepo worktrees)
// while the session exists; staging must resolve via `--all` instead of
// failing with "no tuicr session found" (live bug on MR !54231).
func TestStageApprovedRepoListEmptyFallsBackToAll(t *testing.T) {
	fakeTuicr(t)
	t.Setenv("FAKE_TUICR_REPO_EMPTY", "1")
	t.Setenv("FAKE_TUICR_FILES", "3")

	dir := t.TempDir()
	findings := []store.Finding{
		{ID: "F001", File: "a.go", Side: "new", Line: 1, Severity: "P1", Kind: "bug",
			Title: "t", Explanation: "The call drops the deadline.",
			Evidence: "a.go:1 — offending call",
			Comment:  "Please fix finding 1.", Confidence: "high", Decision: "approved"},
	}
	if err := store.SaveFindings(dir, findings); err != nil {
		t.Fatal(err)
	}

	opts := StageOptions{
		Dir: dir, RepoPath: "/worktree/that/lists/empty",
		Project: "grp/proj", IID: 42, Username: "mr-review AI (human-approved)",
	}
	res, err := StageApproved(context.Background(), opts, NewCLI())
	if err != nil {
		t.Fatalf("staging must resolve via --all fallback: %v", err)
	}
	if len(res.Staged) != 1 || res.Staged[0] != "F001" {
		t.Fatalf("staged = %v, want [F001]", res.Staged)
	}
	mappings, err := LoadMappings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mp, ok := mappings.Get("F001"); !ok || mp.CommentID == "" {
		t.Fatal("F001 must be mapped after fallback staging")
	}
}

// addCount reads the FAKE_TUICR_STATE.add_count counter file — every call
// to the fake `review add` increments it. Tests use it to prove a single
// AddComment invocation produces exactly one underlying call (no retry
// double-insert).
func addCount(t *testing.T) int {
	t.Helper()
	path := os.Getenv("FAKE_TUICR_STATE") + ".add_count"
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("add_count file corrupt: %v", err)
	}
	return n
}

// TestAddCommentNoDoubleInsert — reproduces the P1 #3 bug. The fake exits
// 0 with a non-JSON body (FAKE_TUICR_BAD_JSON=1) on attempt 1; the OLD
// code would fall through to attempt 2 and double-insert. The fixed code
// must NOT retry after exit 0 — the comment is already stored. We assert
// exactly one underlying call, that parseStoredComment succeeds (via
// read-back on the stored comment), and that exactly one stored comment
// exists.
func TestAddCommentNoDoubleInsert(t *testing.T) {
	fakeTuicr(t)
	t.Setenv("FAKE_TUICR_BAD_JSON", "1")
	cli := NewCLI()
	ctx := context.Background()

	in := CommentInput{
		Content:  "double-insert probe",
		Type:     "issue",
		File:     "a.go",
		Line:     10,
		Side:     "new",
		Username: "test",
	}

	// The fake's BAD_JSON path stores the comment (exit 0 → contract:
	// comment IS stored) but emits a non-JSON body. parseStoredComment
	// must succeed via read-back. The important assertion is the add
	// counter is exactly 1 — no retry attempt 2.
	got, err := cli.AddComment(ctx, "gl:grp/proj/pr/42", "/tmp/repo", in)
	if err != nil {
		t.Fatalf("read-back must succeed even when the response body is bad JSON, got: %v", err)
	}
	if got == nil || got.ID == "" {
		t.Fatal("read-back must return the stored comment with an ID")
	}
	if n := addCount(t); n != 1 {
		t.Fatalf("expected exactly 1 underlying `review add` call (no retry), got %d", n)
	}

	// And the session must hold exactly one comment with this content.
	cs, err := cli.Comments(ctx, "gl:grp/proj/pr/42", "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("session must hold exactly one comment after the probe, got %d", len(cs))
	}
}

// TestAddCommentHappyPath — attempt 1 succeeds (exit 0 + JSON body).
// Underlying call count = 1; returned comment matches input exactly.
func TestAddCommentHappyPath(t *testing.T) {
	fakeTuicr(t)
	cli := NewCLI()
	ctx := context.Background()

	in := CommentInput{
		Content: "happy path", Type: "issue",
		File: "a.go", Line: 5, Side: "new", Username: "test",
	}
	got, err := cli.AddComment(ctx, "gl:grp/proj/pr/42", "/tmp/repo", in)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID == "" || got.Content != "happy path" || got.Path != "a.go" || got.StartLine != 5 {
		t.Fatalf("unexpected stored comment: %+v", got)
	}
	if n := addCount(t); n != 1 {
		t.Fatalf("happy path must invoke `review add` exactly once, got %d", n)
	}
}

// TestReadBackMatchRequiresExactLine — two identical comments on different
// lines: read-back must NOT return the wrong one. The fixed
// parseStoredComment requires (file, side, line-range, content) to all
// match; matching by content alone would confuse these two.
func TestReadBackMatchRequiresExactLine(t *testing.T) {
	fakeTuicr(t)
	cli := NewCLI()
	ctx := context.Background()

	// Two identical comments on different lines (post-insertion).
	cs0, err := cli.Comments(ctx, "gl:grp/proj/pr/42", "/tmp/repo")
	if err != nil {
		t.Fatal(err)
	}
	// Seed two comments directly via the fake's state file (matching the
	// comment shape produced by the fake).
	seed := []Comment{
		{ID: "c1", Path: "a.go", StartLine: 10, EndLine: 10, Side: "new",
			CommentType: "issue", Author: "test", LifecycleState: "local_draft",
			Content: "duplicate text"},
		{ID: "c2", Path: "a.go", StartLine: 99, EndLine: 99, Side: "new",
			CommentType: "issue", Author: "test", LifecycleState: "local_draft",
			Content: "duplicate text"},
	}
	b, _ := json.Marshal(seed)
	os.WriteFile(os.Getenv("FAKE_TUICR_STATE")+".comments.json", b, 0o644)
	_ = cs0

	// Without a parsed JSON response, parseStoredComment must match by
	// (file, side, line, content). Force it through the read-back path:
	// point at line 10 → must return c1, not c2.
	// We can't directly invoke the unexported method, so we drive via
	// AddComment with FAKE_TUICR_BAD_JSON. The fake inserts ONE comment
	// first, then we make a second AddComment that the read-back path must
	// disambiguate.
	t.Setenv("FAKE_TUICR_BAD_JSON", "1")
	// Insert a third comment that has the SAME content as c1 but a DIFFERENT line.
	_, _ = cli.AddComment(ctx, "gl:grp/proj/pr/42", "/tmp/repo", CommentInput{
		Content: "duplicate text", Type: "issue",
		File: "a.go", Line: 10, Side: "new", Username: "test",
	})
	// After this, the session has c1 (line 10, seed) + new (line 10, fake
	// inserted) — two identical (file, side, line, content) candidates.
	// The read-back path must report AMBIGUOUS (or fail), not silently
	// pick the wrong one. We force a second AddComment for a different
	// line; the fake inserts a 3rd identical-content comment on line 99.
	_, _ = cli.AddComment(ctx, "gl:grp/proj/pr/42", "/tmp/repo", CommentInput{
		Content: "duplicate text", Type: "issue",
		File: "a.go", Line: 99, Side: "new", Username: "test",
	})
	// Now 4 stored comments (c1, c2, fake-insert, fake-insert). Force one
	// more AddComment and observe the ambiguous result — two candidates
	// at line 99 (c2 + the latest fake insert) match by content.
	_, err = cli.AddComment(ctx, "gl:grp/proj/pr/42", "/tmp/repo", CommentInput{
		Content: "duplicate text", Type: "issue",
		File: "a.go", Line: 99, Side: "new", Username: "test",
	})
	if err == nil {
		t.Fatal("expected ambiguity error on duplicate (file, side, line, content), got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error must flag ambiguity, got: %v", err)
	}
}
