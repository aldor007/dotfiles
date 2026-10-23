// Package publish posts human-approved findings to GitLab.
//
// This is the ONLY component in the workflow with remote write capability,
// and it is invoked exclusively by the human (CLI or the TUI's P key):
//   - only findings with decision == "approved" are considered,
//   - the current MR head SHA must equal the reviewed SHA (freshness gate),
//   - existing discussions are checked first so retries and re-runs never
//     duplicate comments,
//   - partial failures are reported honestly and recorded (publish-log.jsonl);
//     published findings persist their remote discussion/note ids.
package publish

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"mr-review/internal/gitlab"
	"mr-review/internal/store"
)

// Options control one publish run.
type Options struct {
	Dir         string
	DryRun      bool
	Yes         bool // skip the interactive PUBLISH confirmation (tests/CI)
	Summary     bool // also post the general summary note
	SummaryFile string
}

// Result reports what happened, honestly separating outcomes.
type Result struct {
	Published []string // finding ids
	Skipped   []string // "id: reason"
	Failed    []string // "id: reason"
	Summary   bool     // summary note posted
}

// ExitCode maps a Result to a process exit code (0 ok, 3 partial, 1 fatal is
// returned as error by Run).
func (r *Result) ExitCode() int {
	if len(r.Failed) > 0 {
		return 3
	}
	return 0
}

// Run executes the publish flow.
func Run(opts Options) (*Result, error) {
	res := &Result{}

	rev, err := store.LoadReview(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("load review: %w", err)
	}
	findings, err := store.LoadFindings(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("load findings (refusing to publish from a corrupt state): %w", err)
	}

	gl := gitlab.New()
	gl.Host = rev.Host

	// ---- freshness gate ----------------------------------------------------
	mr, err := gl.GetMR(rev.Project, rev.IID)
	if err != nil {
		return nil, fmt.Errorf("fetch MR (state NOT advanced): %w", err)
	}
	if mr.State != "opened" {
		return nil, fmt.Errorf("MR is %s, not opened; refusing to comment", mr.State)
	}
	if mr.DiffRefs.HeadSHA != rev.HeadSHA {
		return nil, fmt.Errorf(
			"MR moved since the review: reviewed %s, current %s — run `mr-review refresh %s` and re-approve before publishing",
			short(rev.HeadSHA), short(mr.DiffRefs.HeadSHA), opts.Dir)
	}
	drefs := mr.DiffRefs

	// ---- exact per-file old/new paths (P2 #6) ------------------------------
	// Inline positions are validated by GitLab against the diff entry's own
	// paths; for renames old_path != new_path, so both must come from the
	// exact MR diff listing, not from the finding's single file field.
	diffs, err := gl.MRDiffs(rev.Project, rev.IID)
	if err != nil {
		return nil, fmt.Errorf("fetch MR diff listing (state NOT advanced): %w", err)
	}
	paths := diffPathsOf(diffs)

	// ---- collect approved, unpublished findings + ready manual comments ----
	var approved []store.Finding
	for _, f := range findings {
		switch {
		case f.Publication != nil:
			res.Skipped = append(res.Skipped, f.ID+": already published")
		case f.Decision == "approved":
			approved = append(approved, f)
		default:
			// pending/rejected never reach GitLab; silence is intentional
		}
	}
	comments, err := store.LoadComments(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("load manual comments (refusing a partial batch): %w", err)
	}
	var ready []store.DraftComment
	for _, c := range comments {
		switch {
		case c.Publication != nil:
			res.Skipped = append(res.Skipped, c.ID+": already published")
		case c.State == store.CommentReady:
			ready = append(ready, c)
		default:
			// held-back drafts never reach GitLab
		}
	}

	summaryText := ""
	if opts.Summary {
		summaryText = rev.SummaryComment
		if opts.SummaryFile != "" {
			b, rerr := os.ReadFile(opts.SummaryFile)
			if rerr != nil {
				return nil, fmt.Errorf("summary file: %w", rerr)
			}
			summaryText = strings.TrimSpace(string(b))
		}
		if summaryText == "" {
			return nil, fmt.Errorf("no summary comment set (edit it in the TUI with S)")
		}
	}

	if len(approved) == 0 && len(ready) == 0 && summaryText == "" {
		if len(res.Skipped) > 0 {
			return res, nil // everything already published — idempotent no-op
		}
		return res, fmt.Errorf("nothing to publish: no approved findings and no ready comments")
	}

	// ---- duplicate pre-check (idempotency across runs/manual posts) --------
	me, err := gl.CurrentUser()
	if err != nil {
		return nil, fmt.Errorf("identify current user: %w", err)
	}
	existing, err := gl.Discussions(rev.Project, rev.IID)
	if err != nil {
		return nil, fmt.Errorf("fetch existing discussions: %w", err)
	}
	seen := map[string]string{} // key -> discussion id
	for _, d := range existing {
		for _, n := range d.Notes {
			if n.Author.Username != me {
				continue
			}
			if n.Position == nil {
				// general note: index under the same key shape draftDupKey uses
				seen["|0|"+normalize(n.Body)] = d.ID
				continue
			}
			key := dupKey(n.Position.NewPath, lineOf(n.Position.NewLine, n.Position.OldLine), n.Body)
			seen[key] = d.ID
		}
	}

	// ---- confirmation ------------------------------------------------------
	if !opts.DryRun && !opts.Yes {
		if err := confirm(approved, ready, summaryText); err != nil {
			return nil, err
		}
	}

	preview := func() {
		fmt.Printf("MR !%d %s (%s)\n", rev.IID, rev.Title, rev.Project)
		for _, f := range approved {
			fmt.Printf("  %s %s %s:%d\n", f.Severity, f.ID, f.File, f.Line)
			fmt.Printf("    %s\n", indent(strings.ReplaceAll(f.EffectiveComment(), "\n", "\n    ")))
		}
		for _, c := range ready {
			if c.General {
				fmt.Printf("  [comment] %s (general MR comment)\n", c.ID)
			} else {
				fmt.Printf("  [comment] %s %s\n", c.ID, c.Location())
			}
			fmt.Printf("    %s\n", indent(strings.ReplaceAll(c.Text, "\n", "\n    ")))
		}
		if summaryText != "" {
			fmt.Printf("  [summary note]\n    %s\n", indent(strings.ReplaceAll(summaryText, "\n", "\n    ")))
		}
	}
	preview()
	if opts.DryRun {
		res.Skipped = append(res.Skipped, "ALL: dry run — nothing was posted")
		return res, nil
	}

	// ---- post inline comments ---------------------------------------------
	for _, f := range approved {
		body := f.EffectiveComment()
		key := dupKey(f.File, f.Line, body)
		if dID, ok := seen[key]; ok {
			res.Skipped = append(res.Skipped, f.ID+": identical comment already exists ("+dID+")")
			if err := markPublished(opts.Dir, findings, f.ID, dID, 0); err != nil {
				return res, err
			}
			continue
		}
		pos := positionFor(f, paths)
		dID, noteID, err := gl.PostDiscussion(rev.Project, rev.IID, body, &drefs, pos)
		if err != nil {
			res.Failed = append(res.Failed, f.ID+": "+err.Error())
			_ = store.AppendPublishLog(opts.Dir, map[string]any{
				"finding": f.ID, "outcome": "failed", "error": err.Error(),
			})
			continue // partial failure: keep posting the rest
		}
		if err := markPublished(opts.Dir, findings, f.ID, dID, noteID); err != nil {
			return res, fmt.Errorf("record publication of %s: %w (comment IS on GitLab)", f.ID, err)
		}
		res.Published = append(res.Published, f.ID)
		_ = store.AppendPublishLog(opts.Dir, map[string]any{
			"finding": f.ID, "outcome": "published",
			"discussion_id": dID, "note_id": noteID, "head_sha": rev.HeadSHA,
		})
	}

	// ---- post manual comments (ready drafts) -----------------------------
	for _, c := range ready {
		if dID, ok := seen[draftDupKey(c)]; ok {
			res.Skipped = append(res.Skipped, c.ID+": identical comment already exists ("+dID+")")
			if err := markDraftPublished(opts.Dir, c.ID, dID, 0); err != nil {
				return res, err
			}
			continue
		}
		pos := draftPositionFor(c, paths)
		dID, noteID, err := gl.PostDiscussion(rev.Project, rev.IID, c.Text, &drefs, pos)
		if err != nil {
			res.Failed = append(res.Failed, c.ID+": "+err.Error())
			_ = store.AppendPublishLog(opts.Dir, map[string]any{
				"comment": c.ID, "outcome": "failed", "error": err.Error(),
			})
			continue // partial failure: keep posting the rest
		}
		if err := markDraftPublished(opts.Dir, c.ID, dID, noteID); err != nil {
			return res, fmt.Errorf("record publication of %s: %w (comment IS on GitLab)", c.ID, err)
		}
		res.Published = append(res.Published, c.ID)
		_ = store.AppendPublishLog(opts.Dir, map[string]any{
			"comment": c.ID, "outcome": "published",
			"discussion_id": dID, "note_id": noteID, "head_sha": rev.HeadSHA,
		})
	}

	// ---- summary note (idempotent, P2 #7) ----------------------------------
	// The summary is a general note posted once: its discussion id is
	// persisted in the review state, and the remote scan above is a second
	// net (an identical general note by us blocks a re-post even if the
	// local state was lost). Neither path can produce a duplicate.
	if summaryText != "" {
		switch {
		case rev.SummaryDiscussionID != "":
			res.Skipped = append(res.Skipped,
				"summary: already published → discussion "+rev.SummaryDiscussionID)
		default:
			if dID, ok := seen["|0|"+normalize(summaryText)]; ok {
				res.Skipped = append(res.Skipped,
					"summary: identical general note already exists ("+dID+")")
				rev.SummaryDiscussionID = dID // heal local state from the remote scan
				if serr := rev.Save(opts.Dir); serr != nil {
					return res, fmt.Errorf("record existing summary discussion: %w", serr)
				}
				break
			}
			dID, _, err := gl.PostDiscussion(rev.Project, rev.IID, summaryText, nil, nil)
			if err != nil {
				res.Failed = append(res.Failed, "summary: "+err.Error())
				break
			}
			res.Summary = true
			rev.SummaryDiscussionID = dID
			if serr := rev.Save(opts.Dir); serr != nil {
				return res, fmt.Errorf("record summary publication: %w (summary IS on GitLab, discussion %s)", serr, dID)
			}
			_ = store.AppendPublishLog(opts.Dir, map[string]any{
				"finding": "summary", "outcome": "published", "discussion_id": dID, "head_sha": rev.HeadSHA,
			})
		}
	}

	return res, nil
}

// Approve is the separate, explicit MR approval action.
func Approve(dir string, yes bool) error {
	rev, err := store.LoadReview(dir)
	if err != nil {
		return err
	}
	if !yes {
		fmt.Printf("Approve MR !%d in %s on %s? Type APPROVE to confirm: ",
			rev.IID, rev.Project, hostOrInferred(rev.Host))
		ans, rerr := bufio.NewReader(os.Stdin).ReadString('\n')
		if rerr != nil || strings.TrimSpace(ans) != "APPROVE" {
			return fmt.Errorf("approval aborted")
		}
	}
	gl := gitlab.New()
	gl.Host = rev.Host
	if err := gl.Approve(rev.Project, rev.IID); err != nil {
		_ = store.AppendPublishLog(dir, map[string]any{"finding": "approval", "outcome": "failed", "error": err.Error()})
		return err
	}
	return store.AppendPublishLog(dir, map[string]any{"finding": "approval", "outcome": "approved"})
}

// ---- helpers ---------------------------------------------------------------

// pathPair carries the exact old/new path pair of one file as it appears in
// the MR diff (P2 #6).
type pathPair struct{ New, Old string }

// diffPathsOf maps every path the MR diff mentions (old AND new side) to the
// exact pair GitLab expects in inline positions. For a rename a.go → b.go
// both keys map to {New: b.go, Old: a.go}, so comments pinned to either side
// of the rename validate.
func diffPathsOf(diffs []gitlab.MRDiff) map[string]pathPair {
	m := make(map[string]pathPair, len(diffs))
	for _, d := range diffs {
		newP, oldP := d.NewPath, d.OldPath
		if newP == "" {
			newP = oldP
		}
		if oldP == "" {
			oldP = newP
		}
		m[newP] = pathPair{New: newP, Old: oldP}
		if oldP != newP {
			m[oldP] = pathPair{New: newP, Old: oldP}
		}
	}
	return m
}

// lookupPaths falls back to treating the path as both its own sides when the
// MR diff has no entry for it (defensive: such a path cannot carry a valid
// position anyway, and keeps behaviour identical to the pre-fix code there).
func lookupPaths(m map[string]pathPair, path string) pathPair {
	if p, ok := m[path]; ok {
		return p
	}
	return pathPair{New: path, Old: path}
}

func positionFor(f store.Finding, paths map[string]pathPair) *gitlab.Position {
	pair := lookupPaths(paths, f.File)
	pos := &gitlab.Position{NewPath: pair.New, OldPath: pair.Old}
	if f.Side == "old" {
		l := f.Line
		pos.OldLine = &l
		return pos
	}
	if f.LineEnd > f.Line {
		le := f.LineEnd
		ls := f.Line
		pos.LineRange = &struct {
			Start *struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			} `json:"start,omitempty"`
			End *struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			} `json:"end,omitempty"`
		}{
			Start: &struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			}{Line: &ls, Type: "new"},
			End: &struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			}{Line: &le, Type: "new"},
		}
		return pos
	}
	l := f.Line
	pos.NewLine = &l
	return pos
}

// draftPositionFor maps a draft comment to a GitLab diff position.
func draftPositionFor(c store.DraftComment, paths map[string]pathPair) *gitlab.Position {
	if c.General {
		return nil
	}
	pair := lookupPaths(paths, c.File)
	pos := &gitlab.Position{NewPath: pair.New, OldPath: pair.Old}
	if c.Side == "old" {
		l := c.Line
		pos.OldLine = &l
		return pos
	}
	if c.LineEnd > c.Line {
		s, e := c.Line, c.LineEnd
		pos.LineRange = &struct {
			Start *struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			} `json:"start,omitempty"`
			End *struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			} `json:"end,omitempty"`
		}{
			Start: &struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			}{Line: &s, Type: "new"},
			End: &struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			}{Line: &e, Type: "new"},
		}
		return pos
	}
	l := c.Line
	pos.NewLine = &l
	return pos
}

// draftDupKey mirrors dupKey for draft comments (general → text-only).
func draftDupKey(c store.DraftComment) string {
	if c.General {
		return "|0|" + normalize(c.Text)
	}
	// lineOf on the remote scan picks NewLine, else OldLine — a draft's Line
	// is the number on its own side, so the same value is compared here.
	return dupKey(c.File, c.Line, c.Text)
}

func markDraftPublished(dir, id, discussionID string, noteID int64) error {
	cs, err := store.LoadComments(dir)
	if err != nil {
		return err
	}
	for i := range cs {
		if cs[i].ID == id {
			cs[i].Publication = &store.Publication{
				DiscussionID: discussionID, NoteID: noteID,
				At: time.Now().UTC().Format(time.RFC3339),
			}
		}
	}
	return store.SaveComments(dir, cs)
}

func markPublished(dir string, findings []store.Finding, id, discussionID string, noteID int64) error {
	for i := range findings {
		if findings[i].ID == id {
			findings[i].Publication = &store.Publication{
				DiscussionID: discussionID, NoteID: noteID,
				At: time.Now().UTC().Format(time.RFC3339),
			}
		}
	}
	return store.SaveFindings(dir, findings)
}

func dupKey(path string, line int, body string) string {
	return path + "|" + fmt.Sprint(line) + "|" + normalize(body)
}

func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func lineOf(newLine, oldLine *int) int {
	if newLine != nil {
		return *newLine
	}
	if oldLine != nil {
		return *oldLine
	}
	return 0
}

// confirm requires the operator to type PUBLISH on a real terminal.
func confirm(approved []store.Finding, ready []store.DraftComment, summaryText string) error {
	total := len(approved) + len(ready)
	desc := fmt.Sprintf("%d inline comment(s)", total)
	if summaryText != "" {
		desc += " + a summary note"
	}
	fmt.Printf("about to post %s.\n", desc)
	fmt.Print("type PUBLISH to confirm: ")
	st, _ := os.Stdin.Stat()
	if st != nil && st.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("refusing to publish without a terminal confirmation (use --yes explicitly)")
	}
	ans, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil || strings.TrimSpace(ans) != "PUBLISH" {
		return fmt.Errorf("publish aborted")
	}
	return nil
}

func indent(s string) string { return s }

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func hostOrInferred(h string) string {
	if h == "" {
		return "gitlab host inferred by glab"
	}
	return h
}
