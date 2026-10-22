// Package export writes review data to JSON or Markdown.
//
// Safety contract:
//   - export never publishes, never approves findings, never changes review
//     state and never calls GitLab (read-only use of local state files),
//   - existing files are never overwritten silently (--force to allow),
//   - writes are atomic (tmp + rename) and use restrictive permissions,
//   - the schema is versioned (schema_version) so exports stay validatable.
package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mr-review/internal/store"
)

// SchemaVersion is the version of the exported document schema.
const SchemaVersion = 1

// Scope selects what to export.
type Scope string

const (
	ScopeAll       Scope = "all" // every finding
	ScopeApproved  Scope = "approved"
	ScopePending   Scope = "pending"
	ScopeRejected  Scope = "rejected"
	ScopePublished Scope = "published"
	ScopeStale     Scope = "stale"
	ScopeSelected  Scope = "selected" // one finding (Options.FindingID)
	ScopeManual    Scope = "manual"   // manual comments only
	ScopeReport    Scope = "report"   // findings + comments + progress
)

func ValidScope(s Scope) bool {
	switch s {
	case ScopeAll, ScopeApproved, ScopePending, ScopeRejected,
		ScopePublished, ScopeStale, ScopeSelected, ScopeManual, ScopeReport:
		return true
	}
	return false
}

// Format of the output document.
const (
	FormatJSON     = "json"
	FormatMarkdown = "markdown"
)

// Options controls one export run.
type Options struct {
	Dir       string
	Scope     Scope
	Format    string // json | markdown
	Out       string // destination path, or "-" for stdout
	Force     bool   // allow overwriting an existing file
	FindingID string // used with ScopeSelected

	IncludeEvidence bool
	IncludeComments bool // manual comments in finding scopes / report
	IncludeRejected bool
	IncludeProgress bool
}

// Result reports what was written.
type Result struct {
	Path     string // "" when stdout
	Findings int
	Comments int
}

// ---- document schema -------------------------------------------------------

// ReviewInfo identifies the reviewed merge request.
type ReviewInfo struct {
	Project      string `json:"project"`
	Host         string `json:"host,omitempty"`
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	Author       string `json:"author,omitempty"`
	SourceBranch string `json:"source_branch,omitempty"`
	TargetBranch string `json:"target_branch,omitempty"`
	State        string `json:"state,omitempty"`
	HeadSHA      string `json:"head_sha"`
	BaseSHA      string `json:"base_sha,omitempty"`
	StartSHA     string `json:"start_sha,omitempty"`
	URL          string `json:"url,omitempty"`
}

// Finding wraps a stored finding with computed export fields.
type Finding struct {
	store.Finding
	Location         string `json:"location"`
	EffectiveComment string `json:"effective_comment"`
}

// Comment wraps a manual draft comment with its location.
type Comment struct {
	store.DraftComment
	Location string `json:"location"`
}

// Progress is the manual review progress at export time.
type Progress struct {
	ReviewedFiles      []string `json:"reviewed_files"`
	ReviewedFilesCount int      `json:"reviewed_files_count"`
	ChangedFilesCount  int      `json:"changed_files_count,omitempty"`
	ReviewedHunks      int      `json:"reviewed_hunks_count,omitempty"`
	Counts             Counts   `json:"counts"`
}

// Counts summarizes finding decisions.
type Counts struct {
	Total     int `json:"total"`
	Approved  int `json:"approved"`
	Pending   int `json:"pending"`
	Rejected  int `json:"rejected"`
	Published int `json:"published"`
	Stale     int `json:"stale"`
}

// Document is the versioned export payload.
type Document struct {
	SchemaVersion int        `json:"schema_version"`
	ExportedAt    string     `json:"exported_at"`
	Review        ReviewInfo `json:"review"`
	Findings      []Finding  `json:"findings,omitempty"`
	Comments      []Comment  `json:"comments,omitempty"`
	Progress      *Progress  `json:"progress,omitempty"`
}

// Build assembles the export document from local state (no network, no side
// effects on review state).
func Build(opts Options) (*Document, error) {
	rev, err := store.LoadReview(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("load review: %w", err)
	}
	findings, err := store.LoadFindings(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("load findings: %w", err)
	}
	comments, err := store.LoadComments(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("load comments: %w", err)
	}

	doc := &Document{
		SchemaVersion: SchemaVersion,
		ExportedAt:    store.Now(),
		Review: ReviewInfo{
			Project: rev.Project, Host: rev.Host, IID: rev.IID,
			Title: rev.Title, Author: rev.Author,
			SourceBranch: rev.SourceBranch, TargetBranch: rev.TargetBranch,
			State: rev.State, HeadSHA: rev.HeadSHA, BaseSHA: rev.BaseSHA,
			StartSHA: rev.StartSHA, URL: MRURL(rev.Host, rev.Project, rev.IID),
		},
	}

	if opts.Scope != ScopeManual {
		sel, err := filterFindings(findings, opts)
		if err != nil {
			return nil, err
		}
		for i := range sel {
			f := &sel[i]
			out := Finding{Location: fileLine(f.File, f.Line, f.LineEnd), EffectiveComment: f.EffectiveComment()}
			out.Finding = *f
			if !opts.IncludeEvidence {
				out.Evidence = ""
			}
			doc.Findings = append(doc.Findings, out)
		}
	}

	includeComments := opts.IncludeComments || opts.Scope == ScopeManual || opts.Scope == ScopeReport
	if includeComments {
		for i := range comments {
			c := &comments[i]
			doc.Comments = append(doc.Comments, Comment{
				DraftComment: *c,
				Location:     c.Location(),
			})
		}
	}

	if opts.IncludeProgress || opts.Scope == ScopeReport {
		counts := Counts{Total: len(findings)}
		for i := range findings {
			f := &findings[i]
			switch {
			case f.Publication != nil:
				counts.Published++
			case f.Decision == "approved":
				counts.Approved++
			case f.Decision == "rejected":
				counts.Rejected++
			default:
				counts.Pending++
			}
			if f.Stale {
				counts.Stale++
			}
		}
		doc.Progress = &Progress{
			ReviewedFiles:      rev.ReviewedFiles,
			ReviewedFilesCount: len(rev.ReviewedFiles),
			ReviewedHunks:      len(rev.ReviewedHunks),
			Counts:             counts,
		}
	}
	return doc, nil
}

// filterFindings applies the scope to the finding list.
func filterFindings(fs []store.Finding, opts Options) ([]store.Finding, error) {
	if opts.Scope == ScopeSelected {
		for i := range fs {
			if fs[i].ID == opts.FindingID {
				return []store.Finding{fs[i]}, nil
			}
		}
		return nil, fmt.Errorf("finding %q not found", opts.FindingID)
	}
	var out []store.Finding
	for i := range fs {
		f := &fs[i]
		include := false
		switch opts.Scope {
		case ScopeAll, ScopeReport:
			include = true
		case ScopeApproved:
			include = f.Decision == "approved"
		case ScopePending:
			include = f.Decision == "pending" && f.Publication == nil
		case ScopeRejected:
			include = f.Decision == "rejected"
		case ScopePublished:
			include = f.Publication != nil
		case ScopeStale:
			include = f.Stale
		}
		if include && !opts.IncludeRejected && f.Decision == "rejected" &&
			(opts.Scope == ScopeAll || opts.Scope == ScopeReport) {
			include = false
		}
		if include {
			out = append(out, *f)
		}
	}
	return out, nil
}

func fileLine(file string, line, lineEnd int) string {
	if lineEnd > line {
		return fmt.Sprintf("%s:%d-%d", file, line, lineEnd)
	}
	return fmt.Sprintf("%s:%d", file, line)
}

// MRURL builds the web URL of the merge request.
func MRURL(host, project string, iid int) string {
	h := host
	if h == "" {
		return "" // glab infers the host; do not guess one here
	}
	h = strings.TrimPrefix(strings.TrimPrefix(h, "https://"), "http://")
	h = strings.TrimSuffix(h, "/")
	return fmt.Sprintf("https://%s/%s/-/merge_requests/%d", h, project, iid)
}

// Run builds and writes the export. Returns the written path ("" for stdout).
func Run(opts Options) (*Result, error) {
	if !ValidScope(opts.Scope) {
		return nil, fmt.Errorf("invalid scope %q", opts.Scope)
	}
	if opts.Format != FormatJSON && opts.Format != FormatMarkdown {
		return nil, fmt.Errorf("invalid format %q (json|markdown)", opts.Format)
	}
	doc, err := Build(opts)
	if err != nil {
		return nil, err
	}

	var payload []byte
	switch opts.Format {
	case FormatJSON:
		payload, err = json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, err
		}
		payload = append(payload, '\n')
	case FormatMarkdown:
		payload = []byte(renderMarkdown(doc, opts))
	}

	res := &Result{Findings: len(doc.Findings), Comments: len(doc.Comments)}
	if opts.Out == "" || opts.Out == "-" {
		if _, err := os.Stdout.Write(payload); err != nil {
			return nil, err
		}
		return res, nil
	}

	// file destination: never overwrite silently, write atomically
	if _, err := os.Stat(opts.Out); err == nil && !opts.Force {
		return nil, fmt.Errorf("refusing to overwrite %s (pass --force or choose another name)", opts.Out)
	}
	if err := atomicWriteFile(opts.Out, payload); err != nil {
		return nil, err
	}
	res.Path = opts.Out
	return res, nil
}

// DefaultFilename returns the safe default destination for a review.
func DefaultFilename(rev *store.Review, format string, scope Scope) string {
	ext := ".json"
	if format == FormatMarkdown {
		ext = ".md"
	}
	suffix := ""
	if scope != ScopeAll && scope != ScopeReport {
		suffix = "-" + string(scope)
	}
	return fmt.Sprintf("mr-%d-%s%s%s", rev.IID, "review", suffix, ext)
}

func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".mr-review-export-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// ---- markdown --------------------------------------------------------------

// renderMarkdown produces a human-readable report grouped by file.
func renderMarkdown(doc *Document, opts Options) string {
	var b strings.Builder
	md := func(s string) string { return escapeMD(s) }
	r := doc.Review
	fmt.Fprintf(&b, "# Review: %s\n\n", md(r.Title))
	fmt.Fprintf(&b, "- MR: [%s !%d](%s)\n", md(r.Project), r.IID, r.URL)
	fmt.Fprintf(&b, "- Branch: `%s` → `%s`\n", md(r.SourceBranch), md(r.TargetBranch))
	fmt.Fprintf(&b, "- Reviewed head: `%s` (base `%s`)\n", md(r.HeadSHA), md(r.BaseSHA))
	fmt.Fprintf(&b, "- Exported: %s (schema v%d)\n\n", doc.ExportedAt, doc.SchemaVersion)

	if doc.Progress != nil {
		p := doc.Progress
		fmt.Fprintf(&b, "## Progress\n\n")
		fmt.Fprintf(&b, "%d finding(s): %d approved, %d pending, %d rejected, %d published, %d stale · %d/%d files reviewed\n\n",
			p.Counts.Total, p.Counts.Approved, p.Counts.Pending, p.Counts.Rejected,
			p.Counts.Published, p.Counts.Stale, p.ReviewedFilesCount, p.ChangedFilesCount)
	}

	if len(doc.Findings) > 0 {
		fmt.Fprintf(&b, "## Findings (%d)\n\n", len(doc.Findings))
		byFile := map[string][]*Finding{}
		var order []string
		for i := range doc.Findings {
			f := &doc.Findings[i]
			if _, ok := byFile[f.File]; !ok {
				order = append(order, f.File)
			}
			byFile[f.File] = append(byFile[f.File], f)
		}
		for _, file := range order {
			fmt.Fprintf(&b, "### `%s`\n\n", md(file))
			for _, f := range byFile[file] {
				state := f.Decision
				if f.Publication != nil {
					state = "published"
				}
				if f.Stale {
					state += " (stale)"
				}
				fmt.Fprintf(&b, "#### %s %s — %s (%s)\n\n", md(f.Severity), md(f.ID), md(f.Title), md(state))
				fmt.Fprintf(&b, "- Location: `%s`\n", md(f.Location))
				fmt.Fprintf(&b, "- Kind: %s · Confidence: %s\n\n", md(f.Kind), md(f.Confidence))
				fmt.Fprintf(&b, "%s\n\n", md(f.Explanation))
				if opts.IncludeEvidence && strings.TrimSpace(f.Evidence) != "" {
					fmt.Fprintf(&b, "**Evidence**\n\n```\n%s\n```\n\n", f.Evidence)
				}
				if strings.TrimSpace(f.EffectiveComment) != "" {
					fmt.Fprintf(&b, "**Suggested comment**\n\n> %s\n\n",
						strings.ReplaceAll(f.EffectiveComment, "\n", "\n> "))
				}
			}
		}
	}

	if len(doc.Comments) > 0 {
		fmt.Fprintf(&b, "## Manual comments (%d)\n\n", len(doc.Comments))
		for _, c := range doc.Comments {
			state := c.State
			if c.Publication != nil {
				state = "published"
			}
			loc := c.Location
			if loc == "" {
				loc = "general MR comment"
			}
			fmt.Fprintf(&b, "### %s %s — %s\n\n> %s\n\n",
				md(c.ID), md(state), md(loc), strings.ReplaceAll(c.Text, "\n", "\n> "))
		}
	}
	return b.String()
}

// escapeMD neutralizes markdown-active characters in untrusted content.
func escapeMD(s string) string {
	s = strings.ReplaceAll(s, "`", "'")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
