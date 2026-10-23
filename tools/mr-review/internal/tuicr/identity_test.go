package tuicr

// Tests for review finding P1-2: publication-related session matching must
// compare the canonical tuicr session identity EXACTLY (forge host, full
// project path, MR IID, and reviewed revision/SHA when the session records
// it). No substring matching anywhere.

import (
	"context"
	"strings"
	"testing"
)

// TestResolveSessionExactProjectMatch — exact match passes; the substring
// bug class is dead: project "team/foo" must NOT resolve the session of
// "team/foobar" with the same MR IID (and vice versa).
func TestResolveSessionExactProjectMatch(t *testing.T) {
	exact := []Session{{Slug: "gl:team/foo/pr/42", Kind: "pr"}}
	if s := ResolveSession(exact, "team/foo", 42); s == nil || s.Slug != "gl:team/foo/pr/42" {
		t.Fatalf("exact project + IID must match, got %+v", s)
	}
	if s := ResolveSession(exact, "team/foo", 43); s != nil {
		t.Fatalf("wrong IID must not match, got %+v", s)
	}
	foobar := []Session{{Slug: "gl:team/foobar/pr/42", Kind: "pr"}}
	if s := ResolveSession(foobar, "team/foo", 42); s != nil {
		t.Fatalf("P1-2 substring bug: project team/foo matched session team/foobar: %+v", s)
	}
	if s := ResolveSession(foobar, "team/fo", 42); s != nil {
		t.Fatalf("project prefix team/fo matched session team/foobar: %+v", s)
	}
	if s := ResolveSession(exact, "team/foobar", 42); s != nil {
		t.Fatalf("longer project must not match shorter session slug: %+v", s)
	}
	// slashy project paths must parse to the exact full path
	nested := []Session{{Slug: "gl:group/sub/proj/pr/7", Kind: "pr"}}
	if s := ResolveSession(nested, "group/sub/proj", 7); s == nil {
		t.Fatal("multi-segment project must match exactly")
	}
	if s := ResolveSession(nested, "group/sub/projx", 7); s != nil {
		t.Fatalf("project suffix extension must not match: %+v", s)
	}
}

// TestResolveSessionRejectsWrongHost — the forge token in the slug is the
// GitLab host identity at this layer: only "gl:" PR sessions may ever
// resolve for a GitLab MR. Also covers the optional RECORDED host field.
func TestResolveSessionRejectsWrongHost(t *testing.T) {
	for _, slug := range []string{
		"gh:team/foo/pr/42",     // GitHub-style forge
		"bb:team/foo/pr/42",     // other forge token
		"team/foo/pr/42",        // missing forge token
		"gitea:team/foo/pr/42",  // prefix-collides with "g..." but is not "gl"
	} {
		ss := []Session{{Slug: slug, Kind: "pr"}}
		if s := ResolveSession(ss, "team/foo", 42); s != nil {
			t.Fatalf("slug %q must be rejected (not a GitLab PR session), got %+v", slug, s)
		}
	}
	// a session that RECORDS a host must match it exactly (scheme/trailing
	// slash normalization only), and a differing host must be rejected with
	// a host diagnostic
	ss := []Session{{Slug: "gl:team/foo/pr/42", Kind: "pr", Host: "gitlab.example.com"}}
	if _, err := ResolveSessionForMR(ss, MRIdentity{Project: "team/foo", IID: 42, Host: "gitlab.com"}); err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("recorded host mismatch must be rejected with a host diagnostic, got %v", err)
	}
	if _, err := ResolveSessionForMR(ss, MRIdentity{Project: "team/foo", IID: 42, Host: "https://GitLab.Example.com/"}); err != nil {
		t.Fatalf("same host with scheme/case/trailing slash must match exactly, got %v", err)
	}
	// no host expectation → recorded host is not verified (cannot fail)
	if _, err := ResolveSessionForMR(ss, MRIdentity{Project: "team/foo", IID: 42}); err != nil {
		t.Fatalf("no host expectation must accept recorded host, got %v", err)
	}
}

// TestResolveSessionRejectsWrongRevision — when the session records a
// reviewed revision/SHA and the caller expects one, they must be equal.
func TestResolveSessionRejectsWrongRevision(t *testing.T) {
	ss := []Session{{Slug: "gl:team/foo/pr/42", Kind: "pr", Revision: "abc123"}}
	if _, err := ResolveSessionForMR(ss, MRIdentity{Project: "team/foo", IID: 42, Revision: "def456"}); err == nil || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("wrong SHA must be rejected with a revision diagnostic, got %v", err)
	}
	if _, err := ResolveSessionForMR(ss, MRIdentity{Project: "team/foo", IID: 42, Revision: "abc123"}); err != nil {
		t.Fatalf("matching SHA must pass, got %v", err)
	}
	// caller does not expect a revision → accepted (nothing to verify)
	if _, err := ResolveSessionForMR(ss, MRIdentity{Project: "team/foo", IID: 42}); err != nil {
		t.Fatalf("no revision expectation must pass, got %v", err)
	}
	// session does not record a revision → accepted safely
	ss2 := []Session{{Slug: "gl:team/foo/pr/42", Kind: "pr"}}
	if _, err := ResolveSessionForMR(ss2, MRIdentity{Project: "team/foo", IID: 42, Revision: "def456"}); err != nil {
		t.Fatalf("absent recorded revision must be skipped safely, got %v", err)
	}
}

// TestResolveSessionMissingIdentityFailsClosed — missing or malformed
// identity fields never match (fail closed), never guess.
func TestResolveSessionMissingIdentityFailsClosed(t *testing.T) {
	session := []Session{{Slug: "gl:team/foo/pr/42", Kind: "pr"}}
	if s := ResolveSession(session, "", 42); s != nil {
		t.Fatal("empty project must never match")
	}
	if s := ResolveSession(session, "team/foo", 0); s != nil {
		t.Fatal("IID 0 must never match")
	}
	if s := ResolveSession(session, "team/foo", -1); s != nil {
		t.Fatal("negative IID must never match")
	}
	if s := ResolveSession(nil, "team/foo", 42); s != nil {
		t.Fatal("empty session list must resolve to nil")
	}
	for _, slug := range []string{
		"",                 // empty
		"gl:",              // no identity
		"gl:team/pr/x",     // non-numeric IID
		"gl:team/pr/0",     // non-positive IID
		"gl:team/foo/42",   // missing "pr" segment
		"gl:/pr/42",        // empty project
		"gl:team/foo/pr/",  // empty IID
		"noteam/foo/pr/42", // no forge token (no colon)
	} {
		ss := []Session{{Slug: slug, Kind: "pr"}}
		if s := ResolveSession(ss, "team/foo", 42); s != nil {
			t.Fatalf("malformed slug %q must be rejected, got %+v", slug, s)
		}
	}
	// local sessions are still never candidates for an MR
	local := []Session{{Slug: "team/foo@main/worktree", Kind: "local", Active: true}}
	if s := ResolveSession(local, "team/foo", 42); s != nil {
		t.Fatalf("local session must never satisfy an MR identity, got %+v", s)
	}
	// ResolveSessionForMR with an incomplete identity: explicit error
	if _, err := ResolveSessionForMR(session, MRIdentity{}); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete MR identity must fail closed with an explicit error, got %v", err)
	}
}

// TestResolveSessionForMRListsRejections — rejection must produce a clear
// error listing every candidate and exactly what did not match.
func TestResolveSessionForMRListsRejections(t *testing.T) {
	sessions := []Session{
		{Slug: "gl:team/foobar/pr/42", Kind: "pr"},
		{Slug: "gl:team/foo/pr/41", Kind: "pr"},
		{Slug: "team/foo@dev/worktree", Kind: "local", Active: true},
	}
	_, err := ResolveSessionForMR(sessions, MRIdentity{Project: "team/foo", IID: 42})
	if err == nil {
		t.Fatal("no candidate matches — must error")
	}
	for _, want := range []string{
		`project="team/foo"`, `iid=42`,
		"gl:team/foobar/pr/42", `project "team/foobar" != "team/foo"`,
		"gl:team/foo/pr/41", "iid 41 != 42",
		"team/foo@dev/worktree", "local sessions are never matched",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must list %q, got:\n%v", want, err)
		}
	}
}

// TestResolveSessionSmartListsRejections — the smart resolver surfaces the
// same exact-match diagnostics through the CLI listing path.
func TestResolveSessionSmartListsRejections(t *testing.T) {
	fakeTuicr(t) // fake exposes exactly one session: gl:grp/proj/pr/42
	_, err := NewCLI().ResolveSessionSmartForMR(context.Background(), "/tmp/repo",
		MRIdentity{Project: "grp/proj", IID: 99})
	if err == nil {
		t.Fatal("MR 99 has no exact-match session — must fail closed")
	}
	for _, want := range []string{"exact-match", "gl:grp/proj/pr/42", "iid 42 != 99"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must contain %q, got: %v", want, err)
		}
	}
}

// TestStageApprovedHonorsIdentityExpectations — staging with Host/Revision
// expectations must still succeed against a session that records neither
// (current tuicr), proving the checks are additive and safe.
func TestStageApprovedHonorsIdentityExpectations(t *testing.T) {
	fakeTuicr(t)
	dir := t.TempDir()
	writeFindings(t, dir, baseFindings())
	opts := stageOpts(dir)
	opts.Revision = "deadbeef" // session records no revision → skipped safely
	res, err := StageApproved(context.Background(), opts, NewCLI())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Staged) != 1 || res.Staged[0] != "F001" {
		t.Fatalf("staging with an unverifiable expectation must proceed, got %v (skipped=%v)", res.Staged, res.Skipped)
	}
}
