// Package gitlab wraps the glab CLI for every GitLab interaction.
//
// Fail-closed rules:
//   - any glab error aborts the caller (callers must never advance local
//     state on a failed fetch),
//   - the wrapper never logs or stores anything token-shaped.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Client runs glab commands. Bin is overridable for tests (stub scripts).
type Client struct {
	Bin     string
	Host    string        // optional: passed as GITLAB_HOST (self-managed instances)
	Timeout time.Duration // per-API-call timeout (0 = default 30s)
	Cache   *DiskCache    // optional short-TTL cache (lists, current user)
	// Log, when set, receives every glab invocation before execution
	// ("glab api projects/...") — used for progress display. POST bodies
	// are truncated by the caller.
	Log func(cmd string)
}

// New builds a Client from env (MR_REVIEW_GLAB overrides the binary,
// MR_REVIEW_API_TIMEOUT the per-call timeout, e.g. "30s").
func New() *Client {
	bin := os.Getenv("MR_REVIEW_GLAB")
	if bin == "" {
		bin = "glab"
	}
	return &Client{
		Bin:     bin,
		Timeout: apiTimeoutFromEnv(),
		Cache:   NewDiskCache(),
	}
}

func apiTimeoutFromEnv() time.Duration {
	if s := os.Getenv("MR_REVIEW_API_TIMEOUT"); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			return d
		}
	}
	return 30 * time.Second
}

// EncPath URL-encodes a project path for the GitLab API.
func EncPath(project string) string { return url.PathEscape(project) }

func (c *Client) env() []string {
	e := os.Environ()
	if c.Host != "" {
		e = append(e, "GITLAB_HOST="+c.Host)
	}
	return e
}

// api runs `glab api [-X METHOD] [--paginate] endpoint [fields...]`.
func (c *Client) api(method string, paginate bool, endpoint string, fields ...string) ([]byte, error) {
	args := []string{"api"}
	if method != "" {
		args = append(args, "-X", method)
	}
	if paginate {
		args = append(args, "--paginate")
	}
	args = append(args, endpoint)
	args = append(args, fields...)
	if c.Log != nil {
		c.Log(strings.Join(args, " "))
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Env = c.env()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		tail := stderr.String()
		if len(tail) > 400 {
			tail = tail[len(tail)-400:]
		}
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("glab api %s: timed out after %s (rate limit? slow instance?) — retry or raise MR_REVIEW_API_TIMEOUT", endpoint, timeout)
		}
		return nil, fmt.Errorf("glab api %s: %w: %s", endpoint, err, strings.TrimSpace(tail))
	}
	return out, nil
}

// apiJSON runs api() and unmarshals into v.
func (c *Client) apiJSON(method string, paginate bool, endpoint string, v any, fields ...string) error {
	out, err := c.api(method, paginate, endpoint, fields...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("glab api %s: bad JSON: %w", endpoint, err)
	}
	return nil
}

// ---- types -----------------------------------------------------------------

type userRef struct {
	Username string `json:"username"`
}

// DiffRefs are the SHAs GitLab computed for the MR's current diff version.
type DiffRefs struct {
	BaseSHA  string `json:"base_sha"`
	StartSHA string `json:"start_sha"`
	HeadSHA  string `json:"head_sha"`
}

// MR is the subset of the GitLab merge request API we rely on.
type MR struct {
	IID             int      `json:"iid"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	Author          userRef  `json:"author"`
	SourceBranch    string   `json:"source_branch"`
	TargetBranch    string   `json:"target_branch"`
	SHA             string   `json:"sha"`
	State           string   `json:"state"`
	DiffRefs        DiffRefs `json:"diff_refs"`
	SourceProjectID int      `json:"source_project_id"`
	ProjectID       int      `json:"project_id"`
	WebURL          string   `json:"web_url"`
	UserNotesCount  int      `json:"user_notes_count"`
	HasConflicts    bool     `json:"has_conflicts"`
	Draft           bool     `json:"draft"`
	HeadPipeline    *struct {
		Status string `json:"status"`
		WebURL string `json:"web_url"`
	} `json:"head_pipeline"`
	// GitLab changed detailed_merge_status between an object {label} and a
	// plain string across versions — accept either, never fail the fetch.
	DetailedMergeStatus FlexString `json:"detailed_merge_status"`
}

// FlexString accepts a JSON string or an object carrying a "label" (or
// "status") field — GitLab APIs flip between shapes across versions.
type FlexString struct {
	Value string
}

// UnmarshalJSON tolerates string | {label} | {status} | null.
func (f *FlexString) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		f.Value = s
		return nil
	}
	var o struct {
		Label  string `json:"label"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(b, &o); err == nil {
		f.Value = o.Label
		if f.Value == "" {
			f.Value = o.Status
		}
		return nil
	}
	f.Value = "" // anything else: degrade, never fail the fetch
	return nil
}

// Position pins a comment to a diff location (inline comment).
type Position struct {
	NewPath   string `json:"new_path"`
	OldPath   string `json:"old_path"`
	NewLine   *int   `json:"new_line,omitempty"`
	OldLine   *int   `json:"old_line,omitempty"`
	LineRange *struct {
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
	} `json:"line_range,omitempty"`
}

// Note is one comment inside a discussion.
type Note struct {
	ID       int64     `json:"id"`
	Author   userRef   `json:"author"`
	Body     string    `json:"body"`
	System   bool      `json:"system"`
	Position *Position `json:"position"`
}

// Discussion is a GitLab discussion (thread).
type Discussion struct {
	ID    string `json:"id"`
	Notes []Note `json:"notes"`
}

// MRListItem is one row of an MR listing.
type MRListItem struct {
	IID          int       `json:"iid"`
	Title        string    `json:"title"`
	Author       userRef   `json:"author"`
	SourceBranch string    `json:"source_branch"`
	TargetBranch string    `json:"target_branch"`
	Labels       []string  `json:"labels"`
	UpdatedAt    time.Time `json:"updated_at"`
	ProjectID    int       `json:"project_id"`
	Reference    string    `json:"-"` // filled from references.full
}

type rawMRListItem struct {
	MRListItem
	References struct {
		Full string `json:"full"`
	} `json:"references"`
}

// ---- reads -----------------------------------------------------------------

// MRDiff is one file entry of the MR's current diff version
// (GET /projects/:id/merge_requests/:iid/diffs).
type MRDiff struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	NewFile     bool   `json:"new_file"`
	RenamedFile bool   `json:"renamed_file"`
	DeletedFile bool   `json:"deleted_file"`
	Diff        string `json:"diff"`
	// TooLarge marks a diff GitLab collapsed or refused to render. Some
	// versions send the explicit flag, others just omit the diff body —
	// both map here (DiffUnavailable). The file still counts as changed;
	// only its raw diff is unavailable.
	TooLarge bool `json:"too_large"`
}

// DiffUnavailable reports whether the raw diff body is unavailable
// (too-large/collapsed entry). Best effort: an empty body is treated as
// collapsed.
func (d MRDiff) DiffUnavailable() bool {
	return d.TooLarge || strings.TrimSpace(d.Diff) == ""
}

// GetMR fetches one merge request.
func (c *Client) GetMR(project string, iid int) (*MR, error) {
	var mr MR
	ep := fmt.Sprintf("projects/%s/merge_requests/%d", EncPath(project), iid)
	if err := c.apiJSON("", false, ep, &mr); err != nil {
		return nil, err
	}
	return &mr, nil
}

// MRDiffs fetches the MR's per-file diff listing, paginated
// (GET /merge_requests/:iid/diffs).
//
// SECURITY/REVIEW (P2 #5): the previous implementation looped the
// single-MR /changes endpoint, which is deprecated and NOT a paginated
// collection — the loop silently returned the same first page over and
// over, and renames were double-counted (old + new path both listed).
// The /diffs endpoint is the supported per-file listing. We loop with
// per_page=100 up to maxPages (≈10,000 files) and break on empty/short
// pages, exactly like Discussions. Collapsed/too-large entries are kept
// (the file counts; MRDiff.DiffUnavailable marks the missing body).
func (c *Client) MRDiffs(project string, iid int) ([]MRDiff, error) {
	var out []MRDiff
	const maxPages = 100 // 10,000 files; beyond that we fail closed instead of
	// silently treating the review as complete.
	const perPage = 100
	for page := 1; page <= maxPages; page++ {
		var pageOut []MRDiff
		ep := fmt.Sprintf("projects/%s/merge_requests/%d/diffs?per_page=%d&page=%d",
			EncPath(project), iid, perPage, page)
		if err := c.apiJSON("", false, ep, &pageOut); err != nil {
			return out, err
		}
		if len(pageOut) == 0 {
			break
		}
		out = append(out, pageOut...)
		if len(pageOut) < perPage {
			break
		}
		if page == maxPages {
			return nil, fmt.Errorf("glab api projects/%s/merge_requests/%d/diffs: page %d was full (100 files); diff list exceeds the operational cap and is incomplete", EncPath(project), iid, page)
		}
	}
	return out, nil
}

// ChangedFiles returns the new-side path of every file changed by the MR,
// exactly ONE entry per file: a rename contributes its new path only (the
// old path stays reachable via MRDiffs, which is what inline-position
// construction must use — P2 #6). Collapsed/too-large diffs still count
// their file.
func (c *Client) ChangedFiles(project string, iid int) ([]string, error) {
	diffs, err := c.MRDiffs(project, iid)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(diffs))
	for _, d := range diffs {
		p := d.NewPath
		if p == "" {
			p = d.OldPath
		}
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, nil
}

// Discussions fetches all discussions of the MR, paginated.
//
// SECURITY/REVIEW (P2 #5): a single-page fetch caps at per_page=100.
// On MRs with more discussions (long-running code reviews with many
// human threads), late pages were silently dropped — duplicate-comment
// detection and the AI's awareness of existing conversation both relied
// on a complete list. Now we loop with per_page=100 up to maxPages
// (default 10, ≈ 1000 discussions) and break on short pages.
func (c *Client) Discussions(project string, iid int) ([]Discussion, error) {
	var out []Discussion
	const maxPages = 10
	const perPage = 100
	for page := 1; page <= maxPages; page++ {
		var pageOut []Discussion
		ep := fmt.Sprintf("projects/%s/merge_requests/%d/discussions?per_page=%d&page=%d",
			EncPath(project), iid, perPage, page)
		if err := c.apiJSON("", false, ep, &pageOut); err != nil {
			return out, err
		}
		if len(pageOut) == 0 {
			break
		}
		out = append(out, pageOut...)
		if len(pageOut) < perPage {
			break
		}
	}
	return out, nil
}

// CurrentUser returns the authenticated glab username (cached: it only
// changes on re-auth).
func (c *Client) CurrentUser() (string, error) {
	if c.Cache != nil {
		if b, ok := c.Cache.Get("user"); ok {
			var u userRef
			if json.Unmarshal(b, &u) == nil && u.Username != "" {
				return u.Username, nil
			}
		}
	}
	var u userRef
	if err := c.apiJSON("", false, "user", &u); err != nil {
		return "", err
	}
	if u.Username == "" {
		return "", fmt.Errorf("glab api user: empty username")
	}
	if c.Cache != nil {
		if b, err := json.Marshal(u); err == nil {
			c.Cache.Put("user", b)
		}
	}
	return u.Username, nil
}

// ListOpenMRs lists a project's open MRs (newest first).
// ListLabeled lists open MRs carrying ALL of the given labels
// (GitLab 'labels=a,b' semantics). Use for team workflows like
// "review everything tagged code-review".
func (c *Client) ListLabeled(project string, labels []string, maxPages int) ([]MRListItem, error) {
	if len(labels) == 0 {
		return c.ListOpenMRs(project, maxPages)
	}
	ep := fmt.Sprintf("projects/%s/merge_requests?state=opened&labels=%s&order_by=updated_at&per_page=100",
		EncPath(project), url.QueryEscape(strings.Join(labels, ",")))
	return c.listMRs(ep, maxPages)
}

func (c *Client) ListOpenMRs(project string, maxPages int) ([]MRListItem, error) {
	return c.listMRs(fmt.Sprintf("projects/%s/merge_requests?state=opened&order_by=updated_at&per_page=100", EncPath(project)), maxPages)
}

// ListReviewRequested lists open MRs where the user is a reviewer (project scope).
func (c *Client) ListReviewRequested(project, username string, maxPages int) ([]MRListItem, error) {
	ep := fmt.Sprintf("projects/%s/merge_requests?state=opened&reviewer_username=%s&order_by=updated_at&per_page=100",
		EncPath(project), url.QueryEscape(username))
	return c.listMRs(ep, maxPages)
}

// ListAssigned lists open MRs where the user is the assignee (project scope).
func (c *Client) ListAssigned(project, username string, maxPages int) ([]MRListItem, error) {
	ep := fmt.Sprintf("projects/%s/merge_requests?state=opened&assignee_username=%s&order_by=updated_at&per_page=100",
		EncPath(project), url.QueryEscape(username))
	return c.listMRs(ep, maxPages)
}

// ListAuthored lists open MRs authored by the user (project scope).
func (c *Client) ListAuthored(project, username string, maxPages int) ([]MRListItem, error) {
	ep := fmt.Sprintf("projects/%s/merge_requests?state=opened&author_username=%s&order_by=updated_at&per_page=100",
		EncPath(project), url.QueryEscape(username))
	return c.listMRs(ep, maxPages)
}

func (c *Client) listMRs(endpoint string, maxPages int) ([]MRListItem, error) {
	// short-TTL disk cache: the picker calls several list endpoints and a
	// re-run seconds later should be instant
	cacheKey := "list:" + endpoint
	if c.Cache != nil {
		if b, ok := c.Cache.Get(cacheKey); ok {
			var out []MRListItem
			if json.Unmarshal(b, &out) == nil {
				return out, nil
			}
		} // corrupt entry: treat as miss
	}
	if maxPages <= 0 {
		maxPages = 1
	}
	var raw []rawMRListItem
	for page := 1; page <= maxPages; page++ {
		ep := fmt.Sprintf("%s&page=%d", endpoint, page)
		var pageOut []rawMRListItem
		if err := c.apiJSON("", false, ep, &pageOut); err != nil {
			return nil, err
		}
		if len(pageOut) == 0 {
			break
		}
		raw = append(raw, pageOut...)
		if len(pageOut) < 100 {
			break
		}
	}
	out := make([]MRListItem, 0, len(raw))
	for _, r := range raw {
		item := r.MRListItem
		item.Reference = r.References.Full
		out = append(out, item)
	}
	if c.Cache != nil {
		if b, err := json.Marshal(out); err == nil {
			c.Cache.Put(cacheKey, b)
		}
	}
	return out, nil
}

// ---- writes (used ONLY by cmd/mr-review-publish) ---------------------------

// positionFields builds the GitLab position parameters for an inline comment.
// A multi-line finding uses line_range; single-line uses new_line/old_line.
func positionFields(drefs DiffRefs, pos *Position) []string {
	fields := []string{
		"-f", "position[position_type]=text",
		"-f", "position[base_sha]=" + drefs.BaseSHA,
		"-f", "position[start_sha]=" + drefs.StartSHA,
		"-f", "position[head_sha]=" + drefs.HeadSHA,
		"-f", "position[new_path]=" + pos.NewPath,
		"-f", "position[old_path]=" + pos.OldPath,
	}
	if pos.LineRange != nil && pos.LineRange.Start != nil && pos.LineRange.End != nil {
		fields = append(fields, lineRangeFields(pos)...)
	} else {
		if pos.NewLine != nil {
			fields = append(fields, "-f", "position[new_line]="+strconv.Itoa(*pos.NewLine))
		}
		if pos.OldLine != nil {
			fields = append(fields, "-f", "position[old_line]="+strconv.Itoa(*pos.OldLine))
		}
	}
	return fields
}

func lineRangeFields(pos *Position) []string {
	f := []string{
		"-f", "position[line_range][start][type]=" + pos.LineRange.Start.Type,
		"-f", "position[line_range][end][type]=" + pos.LineRange.End.Type,
	}
	if pos.LineRange.Start.Line != nil {
		f = append(f, "-f", "position[line_range][start][line]="+strconv.Itoa(*pos.LineRange.Start.Line))
	}
	if pos.LineRange.End.Line != nil {
		f = append(f, "-f", "position[line_range][end][line]="+strconv.Itoa(*pos.LineRange.End.Line))
	}
	return f
}

// PostDiscussion posts a comment (optionally pinned to a diff position) and
// returns the discussion id + first note id.
func (c *Client) PostDiscussion(project string, iid int, body string, drefs *DiffRefs, pos *Position) (string, int64, error) {
	ep := fmt.Sprintf("projects/%s/merge_requests/%d/discussions", EncPath(project), iid)
	fields := []string{"-f", "body=" + body}
	if drefs != nil && pos != nil {
		fields = append(fields, positionFields(*drefs, pos)...)
	}
	var resp Discussion
	if err := c.apiJSON("POST", false, ep, &resp, fields...); err != nil {
		return "", 0, err
	}
	if resp.ID == "" || len(resp.Notes) == 0 {
		return "", 0, fmt.Errorf("glab post discussion: unexpected response (no id/notes)")
	}
	return resp.ID, resp.Notes[0].ID, nil
}

// Approve approves the MR (separate, explicit action only).
func (c *Client) Approve(project string, iid int) error {
	ep := fmt.Sprintf("projects/%s/merge_requests/%d/approve", EncPath(project), iid)
	_, err := c.api("POST", false, ep)
	return err
}

// ProjectPath resolves a project id to its path_with_namespace (fork MRs).
func (c *Client) ProjectPath(id int) (string, error) {
	var p struct {
		PathWithNamespace string `json:"path_with_namespace"`
	}
	if err := c.apiJSON("", false, fmt.Sprintf("projects/%d", id), &p); err != nil {
		return "", err
	}
	if p.PathWithNamespace == "" {
		return "", fmt.Errorf("project %d: empty path", id)
	}
	return p.PathWithNamespace, nil
}
