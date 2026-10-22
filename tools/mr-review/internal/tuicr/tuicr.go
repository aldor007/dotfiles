// Package tuicr integrates with the tuicr review TUI through its supported
// CLI (tuicr review list/add/comments/delete). It never writes tuicr's
// internal session files directly — everything goes through the documented
// JSON commands — so manual and AI-originated comments coexist in tuicr's
// own persistence format.
//
// Safety: these calls are all LOCAL to tuicr's session store. Nothing here
// publishes to GitLab; tuicr's own explicit :submit (or mr-review-publish)
// remains the only remote write path.
package tuicr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CLI runs tuicr review commands. Bin is overridable for tests
// (MR_REVIEW_TUICR env).
type CLI struct {
	Bin string
}

// NewCLI returns a CLI using the configured tuicr binary.
func NewCLI() *CLI {
	bin := os.Getenv("MR_REVIEW_TUICR")
	if bin == "" {
		bin = "tuicr"
	}
	return &CLI{Bin: bin}
}

// Session is one entry of `tuicr review list`.
type Session struct {
	Slug          string `json:"slug"`
	Kind          string `json:"kind"` // local | pr
	Path          string `json:"path"`
	UpdatedAt     string `json:"updated_at"`
	CommentCount  int    `json:"comment_count"`
	ReviewedCount int    `json:"reviewed_count"`
	FileCount     int    `json:"file_count"`
	Anchor        string `json:"anchor"`
	Active        bool   `json:"active"`
}

// Comment mirrors one entry of `tuicr review comments`.
type Comment struct {
	ID             string `json:"id"`
	Location       string `json:"location"`
	Path           string `json:"path"`
	StartLine      int    `json:"start_line"`
	EndLine        int    `json:"end_line"`
	Side           string `json:"side"`
	CommentType    string `json:"comment_type"`
	Author         string `json:"author"`
	LifecycleState string `json:"lifecycle_state"`
	CreatedAt      string `json:"created_at"`
	Content        string `json:"content"`
}

// CommentInput is the payload for `tuicr review add --input`.
type CommentInput struct {
	Content   string `json:"content"`
	Type      string `json:"comment_type,omitempty"`
	File      string `json:"file,omitempty"`
	Line      int    `json:"line,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Side      string `json:"side,omitempty"`
	Username  string `json:"username,omitempty"`
}

// run executes a tuicr command and returns trimmed stdout.
func (c *CLI) run(ctx context.Context, args ...string) (string, error) {
	if c.Bin == "" {
		return "", fmt.Errorf("tuicr binary not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("tuicr %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ListSessions returns sessions for a checkout (its local sessions plus the
// origin repo's PR sessions). Falls back to `--all` with client-side
// filtering when `--repo <path>` matches nothing — worktree checkouts of
// monorepos don't always resolve the way tuicr's repo coordinate expects.
func (c *CLI) ListSessions(ctx context.Context, repo string) ([]Session, error) {
	out, err := c.run(ctx, "review", "list", "--repo", repo)
	if err != nil {
		return nil, err
	}
	sessions, perr := parseSessions(out)
	if perr != nil {
		return nil, fmt.Errorf("parsing tuicr review list: %w (%q)", perr, out)
	}
	if len(sessions) > 0 {
		return sessions, nil
	}
	// fallback: --all, keep only sessions whose slug contains a project
	// segment derived from the checkout path's origin (best effort: keep
	// local sessions whose repo_path is under the same checkout root, plus
	// everything else is filtered by ResolveSession later)
	allOut, aerr := c.run(ctx, "review", "list", "--all")
	if aerr != nil {
		return sessions, nil // empty is a valid answer
	}
	all, perr := parseSessions(allOut)
	if perr != nil {
		return sessions, nil
	}
	return all, nil
}

// ListSessionsAll returns every persisted session across all repos.
func (c *CLI) ListSessionsAll(ctx context.Context) ([]Session, error) {
	out, err := c.run(ctx, "review", "list", "--all")
	if err != nil {
		return nil, err
	}
	return parseSessions(out)
}

// ResolveSessionSmart finds the tuicr session to stage AI findings into.
//
// SECURITY (review finding P1 #2): fail closed. The previous implementation
// fell through to "active local session" and "live pane's session" when no
// PR session existed for the requested MR, which could land AI comments
// into an unrelated review (or, in the live-pane case, into whatever the
// user happens to have open). This routine now REQUIRES an exact match on
// project + MR IID (a PR session whose slug ends with "/pr/<iid>" and whose
// repo coordinate contains the project). When no such session exists,
// return "" with an actionable error — the human must open tuicr on the
// MR explicitly before AI staging can proceed.
func (c *CLI) ResolveSessionSmart(ctx context.Context, repo, project string, iid int) (string, error) {
	// 1. `review list --repo <worktree>` — the normal path
	if sessions, err := c.ListSessions(ctx, repo); err == nil {
		if s := ResolveSession(sessions, project, iid); s != nil {
			return s.Slug, nil
		}
	}
	// 2. `review list --all` — worktree path mismatch (monorepo worktrees
	// can fail the local coordinate lookup). Still requires an exact
	// (project, IID) PR match — no fallback to active/live local sessions.
	all, err := c.ListSessionsAll(ctx)
	if err != nil {
		return "", err
	}
	if s := ResolveSession(all, project, iid); s != nil {
		return s.Slug, nil
	}
	return "", fmt.Errorf("no exact-match PR session found for project=%q iid=%d — open tuicr for the MR first (tuicr pr <MR-URL> or tuicr -r <base>..<head> in the worktree) and try again", project, iid)
}

func parseSessions(out string) ([]Session, error) {
	var sessions []Session
	if err := json.Unmarshal([]byte(out), &sessions); err != nil {
		return nil, fmt.Errorf("parsing tuicr review list: %w", err)
	}
	return sessions, nil
}

// Comments returns the comments of a session. repo resolves LOCAL session
// slugs (PR slugs ignore it; passing it always is safe).
func (c *CLI) Comments(ctx context.Context, session, repo string) ([]Comment, error) {
	out, err := c.run(ctx, "review", "comments", "--session", session, "--repo", repo)
	if err != nil {
		return nil, err
	}
	if out == "" || out == "[]" {
		return nil, nil
	}
	var comments []Comment
	if err := json.Unmarshal([]byte(out), &comments); err != nil {
		return nil, fmt.Errorf("parsing tuicr review comments: %w (%q)", err, out)
	}
	return comments, nil
}

// AddComment inserts one comment and returns the stored version (with id).
// Tries the documented JSON stdin form first; if tuicr rejects it, retries
// with the explicit-flag form (some versions validate the two paths
// differently). The stderr of both attempts is surfaced on failure so the
// real reason (file not in diff, bad line, unknown type, …) is visible.
//
// Idempotency: a retry is ONLY attempted when attempt 1's exit code was
// non-zero (so we know nothing was stored). The previous implementation
// retried whenever parseStoredComment failed, which double-inserted the
// same comment whenever the JSON parse path failed after a successful
// insert. Both attempts must be safe to call from concurrent callers.
func (c *CLI) AddComment(ctx context.Context, session, repo string, in CommentInput) (*Comment, error) {
	payload, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}

	// attempt 1: JSON on stdin
	cmd := exec.CommandContext(ctx, c.Bin, "review", "add", "--session", session, "--repo", repo, "--input", "-")
	cmd.Stdin = strings.NewReader(string(payload))
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		// exit 0 → tuicr accepted the input. The comment IS in the session.
		// Whether or not we can parse the response, we must NOT retry.
		if stored, perr := c.parseStoredComment(ctx, session, repo, stdout.String(), in); perr == nil {
			return stored, nil
		} else {
			return nil, fmt.Errorf("tuicr review add (json) accepted but parse failed: %w — comment IS stored; verify with: tuicr review comments --session %s", perr, session)
		}
	}
	firstErr := fmt.Errorf("json: %w — tuicr said: %s", err, strings.TrimSpace(stderr.String()))

	// attempt 2: explicit flags (positional comment text). Only reached
	// when attempt 1 exited non-zero, so we know nothing was stored.
	args := []string{"review", "add", "--session", session, "--repo", repo, "--username", in.Username}
	if in.Type != "" {
		args = append(args, "--type", in.Type)
	}
	if in.File != "" {
		args = append(args, "--target-file", in.File)
	}
	if in.Line > 0 {
		args = append(args, "--line", fmt.Sprint(in.Line))
	}
	if in.EndLine > in.Line {
		args = append(args, "--end-line", fmt.Sprint(in.EndLine))
	}
	if in.Side != "" {
		args = append(args, "--side", in.Side)
	}
	args = append(args, in.Content)
	cmd2 := exec.CommandContext(ctx, c.Bin, args...)
	var stdout2, stderr2 strings.Builder
	cmd2.Stdout = &stdout2
	cmd2.Stderr = &stderr2
	if err2 := cmd2.Run(); err2 != nil {
		return nil, fmt.Errorf("tuicr review add failed twice: json: %v; flags: %w — tuicr said: %s",
			firstErr, err2, strings.TrimSpace(stderr2.String()))
	}
	stored, perr := c.parseStoredComment(ctx, session, repo, stdout2.String(), in)
	if perr != nil {
		return nil, fmt.Errorf("tuicr review add (flags) accepted but parse failed: %w — comment IS stored; verify with: tuicr review comments --session %s", perr, session)
	}
	return stored, nil
}

// parseStoredComment parses an add response, falling back to a read-back
// match when the binary prints something other than JSON.
//
// Read-back reconcile (review finding P1 #3): when the JSON parse fails,
// match by (file, side, line-range, content) — NOT just (file, content) —
// so two distinct comments with identical text on different lines are not
// confused. If exactly one stored comment matches, return it. Zero or many
// matches is an error (zero: nothing was inserted; many: ambiguous — the
// caller should investigate before retrying).
func (c *CLI) parseStoredComment(ctx context.Context, session, repo, out string, in CommentInput) (*Comment, error) {
	trimmed := strings.TrimSpace(out)
	var stored Comment
	if trimmed != "" && json.Unmarshal([]byte(trimmed), &stored) == nil && stored.ID != "" {
		return &stored, nil
	}
	cs, err := c.Comments(ctx, session, repo)
	if err != nil {
		return nil, err
	}
	want := normalizeText(in.Content)
	wantPath := in.File
	wantSide := in.Side
	wantStart, wantEnd := in.Line, in.EndLine
	if wantEnd == 0 || wantEnd < wantStart {
		wantEnd = wantStart
	}
	var hit *Comment
	for i := range cs {
		cand := &cs[i]
		if wantPath != "" && cand.Path != wantPath {
			continue
		}
		if wantSide != "" && cand.Side != wantSide {
			continue
		}
		// when the input pins a line, the stored comment must overlap that line
		if wantStart > 0 {
			cstart, cend := cand.StartLine, cand.EndLine
			if cend == 0 || cend < cstart {
				cend = cstart
			}
			if wantStart > cend || wantEnd < cstart {
				continue
			}
		}
		if normalizeText(cand.Content) != want {
			continue
		}
		if hit != nil {
			return nil, fmt.Errorf("tuicr review add: ambiguous read-back match (%d+ candidates share file/side/line/content) — will not silently pick one; reconcile manually", 2)
		}
		hit = cand
	}
	if hit == nil {
		return nil, fmt.Errorf("tuicr review add: comment not found after insert (output: %q)", trimmed)
	}
	return hit, nil
}

// DeleteComment removes one comment by id.
func (c *CLI) DeleteComment(ctx context.Context, session, commentID, repo string) error {
	_, err := c.run(ctx, "review", "delete", "--session", session, "--comment-id", commentID, "--repo", repo)
	return err
}

// ActiveSession is one entry of tuicr's active_sessions.json — a tuicr TUI
// currently running with that session open.
type ActiveSession struct {
	PID        int    `json:"pid"`
	Slug       string `json:"slug"`
	Path       string `json:"path"`
	LastSeenAt string `json:"last_seen_at"`
}

// ActiveSessions reads tuicr's active_sessions.json (live panes). dir is
// the review dir — the file lives in tuicr's app-support reviews/ root, so
// the session path of any known session is used to locate it.
func (c *CLI) ActiveSessions(knownSessionPath string) ([]ActiveSession, error) {
	if knownSessionPath == "" {
		return nil, nil
	}
	root := filepath.Dir(knownSessionPath) // .../reviews/sessions
	b, err := os.ReadFile(filepath.Join(filepath.Dir(root), "active_sessions.json"))
	if err != nil {
		return nil, nil // no active sessions file: fine
	}
	var parsed struct {
		Sessions []ActiveSession `json:"sessions"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, nil
	}
	return parsed.Sessions, nil
}

// Clear empties a session (comments + marks). tuicr removes the session
// file once it is empty — used to force a rebuild of stale/partial sessions.
func (c *CLI) Clear(ctx context.Context, session, repo string) error {
	_, err := c.run(ctx, "review", "clear", "--session", session, "--repo", repo)
	return err
}

// ResolveSession picks the tuicr session for a GitLab MR review.
//
// SECURITY (review finding P1 #2): fail closed. The only session that
// satisfies the "review this MR" requirement is a PR session whose slug
// ends with "/pr/<iid>" and whose repo coordinate contains the project
// path. Local sessions (worktree/branch-bound reviews) NEVER satisfy an
// MR IID requirement — returning one would risk staging findings into a
// different review the human has open.
//
// Returns nil when no such PR session exists.
func ResolveSession(sessions []Session, project string, iid int) *Session {
	wantSuffix := fmt.Sprintf("/pr/%d", iid)
	for i := range sessions {
		s := &sessions[i]
		if s.Kind != "pr" {
			continue // local/branch sessions can never match an MR IID
		}
		if !strings.HasSuffix(s.Slug, wantSuffix) {
			continue
		}
		if !slugHasProject(s.Slug, project) {
			continue
		}
		return s
	}
	return nil
}

// SessionStatus describes how a session matches the MR the caller asked for.
type SessionStatus int

const (
	// SessionStatusMissing means no session was found for the MR (the human
	// has not opened tuicr yet, or the slug does not match).
	SessionStatusMissing SessionStatus = iota
	// SessionStatusReady means a session was found and its file count
	// matches the expected diff size — the pane can attach directly.
	SessionStatusReady
	// SessionStatusPartial means the session exists but appears incomplete
	// (file count below expected). tuicr builds sessions incrementally and
	// an interrupted build can persist this way. mr-review WILL NOT clear
	// the session automatically — doing so can erase human review work.
	// The caller surfaces this state to the user; the user decides
	// whether to open the session in tuicr and clear it themselves.
	SessionStatusPartial
)

// PrepareSession resolves the tuicr session for an MR and classifies it.
//
// IMPORTANT: PrepareSession never clears or modifies a session. The
// previous implementation called `tuicr review clear` whenever the
// session's file count was below expected, which silently destroyed
// human review work whenever the count drifted (the most common case:
// tuicr filters or omits files in some diffs; the caller's expected
// count comes from GitLab's mr.changes, which can disagree). If the
// session is partial, the caller is told via SessionStatusPartial and
// is expected to surface that to the user — clearing is an explicit
// human decision, taken inside the tuicr pane or via
// `tuicr review clear --session <slug>`.
//
// Returns the slug ("" when no session exists yet — the pane spawn
// creates it), the status, and any error.
func (c *CLI) PrepareSession(ctx context.Context, repo, project string, iid, expectedFiles int) (slug string, status SessionStatus, err error) {
	sessions, err := c.ListSessions(ctx, repo)
	if err != nil {
		return "", SessionStatusMissing, err
	}
	s := ResolveSession(sessions, project, iid)
	if s == nil {
		return "", SessionStatusMissing, nil
	}
	if expectedFiles > 0 && s.FileCount < expectedFiles {
		return s.Slug, SessionStatusPartial, nil
	}
	return s.Slug, SessionStatusReady, nil
}

// slugHasProject reports whether a forge slug ("gl:group/sub/proj/pr/7")
// contains the project path segments.
func slugHasProject(slug, project string) bool {
	if project == "" {
		return true
	}
	norm := strings.NewReplacer("_", "/", ".", "/").Replace(strings.ToLower(slug))
	for _, seg := range strings.Split(strings.ToLower(project), "/") {
		if seg != "" && !strings.Contains(norm, seg) {
			return false
		}
	}
	return true
}
