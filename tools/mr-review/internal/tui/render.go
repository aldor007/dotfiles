package tui

import (
	"fmt"
	"sort"
	"strings"

	"mr-review/internal/store"
)

// Filter narrows the navigator list.
type Filter struct {
	Kind     string // all|pending|approved|rejected|published|stale
	Severity string // "" = any, P0..P3
	File     string // "" = any (substring match)
	Text     string // free-text search (substring on id/title/file)
}

func (fl Filter) Match(f *store.Finding) bool {
	switch fl.Kind {
	case "pending":
		if f.Decision != "pending" || f.Publication != nil {
			return false
		}
	case "approved":
		if f.Decision != "approved" {
			return false
		}
	case "rejected":
		if f.Decision != "rejected" {
			return false
		}
	case "published":
		if f.Publication == nil {
			return false
		}
	case "stale":
		if !f.Stale {
			return false
		}
	}
	if fl.Severity != "" && f.Severity != fl.Severity {
		return false
	}
	if fl.File != "" && !strings.Contains(f.File, fl.File) {
		return false
	}
	if fl.Text != "" {
		hay := strings.ToLower(f.ID + " " + f.Title + " " + f.File + " " + f.Evidence)
		if !strings.Contains(hay, strings.ToLower(fl.Text)) {
			return false
		}
	}
	return true
}

func (fl Filter) Describe() string {
	parts := []string{fl.Kind}
	if fl.Severity != "" {
		parts = append(parts, fl.Severity)
	}
	if fl.File != "" {
		parts = append(parts, "file~"+fl.File)
	}
	if fl.Text != "" {
		parts = append(parts, "/"+fl.Text)
	}
	return strings.Join(parts, "/")
}

type filterOpt struct {
	label string
	f     Filter
}

func filterOptions() []filterOpt {
	fs := []filterOpt{
		{"all", Filter{Kind: "all"}},
		{"pending", Filter{Kind: "pending"}},
		{"approved", Filter{Kind: "approved"}},
		{"rejected", Filter{Kind: "rejected"}},
		{"published", Filter{Kind: "published"}},
		{"stale", Filter{Kind: "stale"}},
	}
	for _, s := range []string{"P0", "P1", "P2", "P3"} {
		fs = append(fs, filterOpt{label: "severity " + s, f: Filter{Kind: "all", Severity: s}})
	}
	return fs
}

// sortFindings orders by severity then ID (stable insertion sort, small N).
func sortFindings(fs []store.Finding) {
	for i := 1; i < len(fs); i++ {
		for j := i; j > 0; j-- {
			a, b := &fs[j-1], &fs[j]
			ra, rb := store.SeverityRank(a.Severity), store.SeverityRank(b.Severity)
			if ra < rb || (ra == rb && a.ID <= b.ID) {
				break
			}
			fs[j-1], fs[j] = fs[j], fs[j-1]
		}
	}
}

// RenderSummary builds the summary overlay text: decisions, manual review
// progress and the publication batch in one place.
func RenderSummary(rev *store.Review, fs []store.Finding, cs []store.DraftComment) string {
	var ap, pe, rj, pub, stale int
	for i := range fs {
		switch {
		case fs[i].Publication != nil:
			pub++
		case fs[i].Decision == "approved":
			ap++
		case fs[i].Decision == "rejected":
			rj++
		default:
			pe++
		}
		if fs[i].Stale {
			stale++
		}
	}
	var cReady, cHeld, cPub int
	for i := range cs {
		switch {
		case cs[i].Publication != nil:
			cPub++
		case cs[i].State == store.CommentReady:
			cReady++
		default:
			cHeld++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "!%d  %s\n", rev.IID, rev.Title)
	fmt.Fprintf(&b, "%s → %s   head %s   author %s\n", rev.SourceBranch, rev.TargetBranch, short(rev.HeadSHA), rev.Author)
	fmt.Fprintf(&b, "\n%d findings: %d approved, %d pending, %d rejected, %d published", len(fs), ap, pe, rj, pub)
	if stale > 0 {
		fmt.Fprintf(&b, ", %d stale", stale)
	}
	fmt.Fprintf(&b, "\n%d manual comments: %d ready, %d held, %d published", len(cs), cReady, cHeld, cPub)
	fmt.Fprintf(&b, "\n%d/%d changed files reviewed", len(rev.ReviewedFiles), len(rev.ReviewedFiles))
	fmt.Fprintf(&b, "\n\npublishing: press P — only approved findings and ready comments are offered,\nand mr-review-publish asks for explicit confirmation.\n")
	return b.String()
}

func short(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func truncPlain(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// sortFindingsMode sorts the filtered subset in place:
// severity (P0→P3, then ID) | file (path, line) | decision (pending,
// approved, rejected; published last).
func sortFindingsMode(fs []store.Finding, filtered []int, mode int) {
	decisionRank := map[string]int{"pending": 0, "approved": 1, "rejected": 2}
	sort.SliceStable(filtered, func(a, b int) bool {
		x, y := &fs[filtered[a]], &fs[filtered[b]]
		switch mode {
		case 1: // file
			if x.File != y.File {
				return x.File < y.File
			}
			return x.Line < y.Line
		case 2: // decision
			// published findings sink to the bottom
			xp, yp := x.Publication != nil, y.Publication != nil
			if xp != yp {
				return !xp
			}
			dx, dy := decisionRank[x.Decision], decisionRank[y.Decision]
			if dx != dy {
				return dx < dy
			}
		}
		// severity (default), ID as tiebreaker
		ra, rb := store.SeverityRank(x.Severity), store.SeverityRank(y.Severity)
		if ra != rb {
			return ra < rb
		}
		return x.ID < y.ID
	})
}

// sevChip renders the severity badge (plain text under NO_COLOR).
func sevChip(f *store.Finding) string {
	if plainMode() {
		return f.Severity
	}
	return sevStyle(f.Severity).Render(f.Severity)
}

// stripANSIish removes SGR escape sequences (for inverse-row re-rendering).
func stripANSIish(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				i = j
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
