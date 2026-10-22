// Package store holds the local review state: review.json metadata,
// findings.jsonl (the single source of truth for findings and human
// decisions), and the path layout under MR_REVIEW_HOME.
//
// Security notes:
//   - no credentials are ever written here (glab reads tokens from its
//     keyring; nothing token-shaped is persisted),
//   - finding file paths are validated against traversal (see Validate),
//   - agent output is treated as untrusted data: everything must pass the
//     strict schema before it is stored, and nothing from it is executed.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Env overrides (used by tests to redirect all state).
const (
	EnvHome = "MR_REVIEW_HOME"
)

// Home returns the review state root (default ~/.local/share/mr-review).
func Home() string {
	if h := os.Getenv(EnvHome); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local/share/mr-review")
}

// WorktreeRoot returns the root for review worktrees.
func WorktreeRoot() string { return filepath.Join(Home(), "worktrees") }

// ReviewDir returns the state dir for one review.
func ReviewDir(project string, iid int) string {
	return filepath.Join(Home(), Slug(project), "mr"+strconv.Itoa(iid))
}

// Slug makes a filesystem-safe project slug: "group/sub/proj" -> "group_sub_proj".
func Slug(project string) string {
	var b strings.Builder
	for _, r := range project {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Review is the metadata record for one review (review.json).
type Review struct {
	Project        string   `json:"project"`
	Host           string   `json:"host"`
	IID            int      `json:"iid"`
	Title          string   `json:"title"`
	Author         string   `json:"author"`
	SourceBranch   string   `json:"source_branch"`
	TargetBranch   string   `json:"target_branch"`
	HeadSHA        string   `json:"head_sha"`
	BaseSHA        string   `json:"base_sha"`
	StartSHA       string   `json:"start_sha"`
	Worktree       string   `json:"worktree"`
	RepoPath       string   `json:"repo_path"`
	SourceProject  string   `json:"source_project,omitempty"` // full path; set for fork MRs
	State          string   `json:"state"`                    // reviewing | published
	SummaryComment string   `json:"summary_comment,omitempty"`
	Description    string   `json:"description,omitempty"`    // MR description (background re-run needs it)
	ReviewedFiles  []string `json:"reviewed_files,omitempty"` // manual review progress (per new path)
	ReviewedHunks  []string `json:"reviewed_hunks,omitempty"` // "path|newStart" per hunk
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

// LoadReview reads review.json from a review dir.
func LoadReview(dir string) (*Review, error) {
	b, err := os.ReadFile(filepath.Join(dir, "review.json"))
	if err != nil {
		return nil, err
	}
	var r Review
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("review.json: %w", err)
	}
	if r.Project == "" || r.IID == 0 || r.HeadSHA == "" {
		return nil, errors.New("review.json: missing project/iid/head_sha")
	}
	return &r, nil
}

// Save writes review.json atomically and bumps UpdatedAt.
func (r *Review) Save(dir string) error {
	r.UpdatedAt = now()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "review.json"), b)
}

// Finding is one AI review finding plus its human state.
type Finding struct {
	ID            string            `json:"id"`
	File          string            `json:"file"`
	Side          string            `json:"side"` // new | old
	Line          int               `json:"line"`
	LineEnd       int               `json:"line_end"`
	Severity      string            `json:"severity"` // P0..P3
	Kind          string            `json:"kind"`     // bug | likely | question
	Title         string            `json:"title"`
	Explanation   string            `json:"explanation"`
	Evidence      string            `json:"evidence"`
	Comment       string            `json:"comment"`
	Confidence    string            `json:"confidence"` // high | medium | low
	Decision      string            `json:"decision"`   // pending | approved | rejected
	EditedComment string            `json:"edited_comment"`
	Discussion    []DiscussionTurn  `json:"discussion"`
	Publication   *Publication      `json:"publication"`
	Stale         bool              `json:"stale,omitempty"` // set by refresh
	Extra         map[string]string `json:"extra,omitempty"`
	UpdatedAt     int64             `json:"updated_at,omitempty"`
}

// DiscussionTurn is one human question and the agent's answer.
type DiscussionTurn struct {
	Q  string `json:"q"`
	A  string `json:"a"`
	At string `json:"at"`
}

// Publication records a successful remote write (never the request payload
// of failed attempts; failures land in publish-log.jsonl only).
type Publication struct {
	DiscussionID string `json:"discussion_id"`
	NoteID       int64  `json:"note_id"`
	At           string `json:"at"`
}

// EffectiveComment returns the comment text that would be published: the
// human-edited version when present, else the agent's suggestion.
func (f *Finding) EffectiveComment() string {
	if strings.TrimSpace(f.EditedComment) != "" {
		return f.EditedComment
	}
	return f.Comment
}

// EffectiveSeverity orders findings for display.
var severityOrder = map[string]int{"P0": 0, "P1": 1, "P2": 2, "P3": 3}

// SeverityRank returns the sort rank of a severity string (unknown last).
func SeverityRank(s string) int {
	if r, ok := severityOrder[s]; ok {
		return r
	}
	return 99
}

var (
	reAbsPath   = regexp.MustCompile(`^/`)
	reBackslash = regexp.MustCompile(`\\`)
	reDotSeg    = regexp.MustCompile(`(^|/)\.\.?(/|$)`)
)

// Validate checks a finding against the schema and rejects unsafe paths.
// It is the ONLY gate between agent output and the local store.
func (f *Finding) Validate() error {
	switch {
	case strings.TrimSpace(f.File) == "":
		return errors.New("missing file")
	case reAbsPath.MatchString(f.File), reBackslash.MatchString(f.File),
		reDotSeg.MatchString(f.File):
		return fmt.Errorf("unsafe file path %q", f.File)
	case strings.TrimSpace(f.Title) == "":
		return errors.New("missing title")
	case strings.TrimSpace(f.Comment) == "":
		return errors.New("missing comment")
	case strings.TrimSpace(f.Evidence) == "":
		return errors.New("missing evidence")
	case strings.TrimSpace(f.Explanation) == "":
		return errors.New("missing explanation")
	case !regexp.MustCompile(`^P[0-3]$`).MatchString(f.Severity):
		return fmt.Errorf("bad severity %q", f.Severity)
	case f.Line < 1:
		return fmt.Errorf("bad line %d", f.Line)
	}
	if f.LineEnd < f.Line {
		f.LineEnd = f.Line
	}
	switch f.Side {
	case "":
		f.Side = "new"
	case "new", "old":
	default:
		return fmt.Errorf("bad side %q", f.Side)
	}
	switch f.Kind {
	case "":
		f.Kind = "bug"
	case "bug", "likely", "question":
	default:
		return fmt.Errorf("bad kind %q", f.Kind)
	}
	switch f.Confidence {
	case "":
		f.Confidence = "medium"
	case "high", "medium", "low":
	default:
		return fmt.Errorf("bad confidence %q", f.Confidence)
	}
	switch f.Decision {
	case "":
		f.Decision = "pending"
	case "pending", "approved", "rejected":
	default:
		return fmt.Errorf("bad decision %q", f.Decision)
	}
	return nil
}

// LoadFindings reads findings.jsonl and re-validates every line (fail-closed:
// one corrupt line aborts the load).
func LoadFindings(dir string) ([]Finding, error) {
	path := filepath.Join(dir, "findings.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Finding
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 8*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var fd Finding
		if err := json.Unmarshal([]byte(line), &fd); err != nil {
			return nil, fmt.Errorf("findings.jsonl:%d: %w", n, err)
		}
		if err := fd.Validate(); err != nil {
			return nil, fmt.Errorf("findings.jsonl:%d: %w", n, err)
		}
		if fd.ID == "" {
			return nil, fmt.Errorf("findings.jsonl:%d: missing id", n)
		}
		out = append(out, fd)
	}
	return out, sc.Err()
}

// SaveFindings writes findings.jsonl atomically.
func SaveFindings(dir string, fs []Finding) error {
	var b strings.Builder
	for _, fd := range fs {
		j, err := json.Marshal(fd)
		if err != nil {
			return err
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	return atomicWrite(filepath.Join(dir, "findings.jsonl"), []byte(b.String()))
}

// FindingsPath returns the findings.jsonl path.
func FindingsPath(dir string) string { return filepath.Join(dir, "findings.jsonl") }

// RawFindingsPath is where the agent is told to write its raw output.
func RawFindingsPath(dir string) string { return filepath.Join(dir, "findings.raw.jsonl") }

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// Now returns the canonical UTC timestamp used in state files (exported for
// the TUI and exporters).
func Now() string { return now() }

// AppendPublishLog appends one JSON line to publish-log.jsonl.
func AppendPublishLog(dir string, entry map[string]any) error {
	entry["at"] = now()
	j, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	fh, err := os.OpenFile(filepath.Join(dir, "publish-log.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = fh.Write(append(j, '\n'))
	return err
}
