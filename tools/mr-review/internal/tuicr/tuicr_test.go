package tuicr

import (
	"context"

	"encoding/json"
	"fmt"
	"mr-review/internal/store"
	"os"
	"path/filepath"
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
    payload = json.loads(sys.stdin.read())
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
	if err == nil || !strings.Contains(err.Error(), "open tuicr first") {
		t.Fatalf("expected actionable no-session error, got %v", err)
	}
	if _, err := LoadMappings(dir); err != nil {
		t.Fatal(err)
	}
}

// TestResolveSessionPriority — PR session for the MR wins; the active local
// session is the fallback; no session at all resolves to nil.
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
	if s := ResolveSession(sessions, "grp/proj", 99); s == nil || !s.Active {
		t.Fatalf("active local session must be the fallback: %+v", s)
	}
	if s := ResolveSession(nil, "grp/proj", 42); s != nil {
		t.Fatalf("empty list must resolve to nil, got %+v", s)
	}
}

// TestPrepareSessionClearsPartial — a session whose file count is below the
// review's diff is cleared (stale partial build); a complete one is kept.
func TestPrepareSessionClearsPartial(t *testing.T) {
	fakeTuicr(t)
	cli := NewCLI()
	ctx := context.Background()

	// stale: 3 files in session, review expects 10
	t.Setenv("FAKE_TUICR_FILES", "3")
	slug, cleared, err := cli.PrepareSession(ctx, "/tmp/repo", "grp/proj", 42, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !cleared || slug != "gl:grp/proj/pr/42" {
		t.Fatalf("partial session must be cleared: slug=%q cleared=%v", slug, cleared)
	}
	// the fake deleted its state file → session gone
	sessions, _ := cli.ListSessions(ctx, "/tmp/repo")
	if len(sessions) != 0 {
		t.Fatalf("cleared session must be removed, got %d", len(sessions))
	}

	// complete: 10 files, review expects 10 → kept (fresh session, no marker)
	t.Setenv("FAKE_TUICR_FILES", "10")
	os.Remove(os.Getenv("FAKE_TUICR_STATE") + ".cleared")
	slug, cleared, err = cli.PrepareSession(ctx, "/tmp/repo", "grp/proj", 42, 10)
	if err != nil {
		t.Fatal(err)
	}
	if cleared || slug == "" {
		t.Fatalf("complete session must be kept: slug=%q cleared=%v", slug, cleared)
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
			Comment: "Please fix finding 1.", Confidence: "high", Decision: "approved"},
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
