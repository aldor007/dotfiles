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
//
// SECURITY (review finding P1 #7): host is part of the path AND the slug
// uses a collision-safe encoding. Without the latter, two distinct
// projects on the SAME host collide — e.g. "group/sub/proj" and
// "group.sub.proj" both sanitized to "group_sub_proj", and their
// publication state / findings could overwrite each other. DefaultHost
// is used when the caller doesn't know the host (callers that DO know
// it must pass it explicitly).
func ReviewDir(host, project string, iid int) string {
	return filepath.Join(Home(), Slug(host, project), "mr"+strconv.Itoa(iid))
}

// DefaultHost is the GitLab host used when the caller doesn't supply one
// (legacy callers, review references without an explicit Host field).
const DefaultHost = "gitlab.com"

// Slug makes a filesystem-safe, collision-safe (host, project) slug.
//
// Layout: "<host>_<project-escaped>" — the single underscore between
// host and project is the ONLY separator, and the project portion
// escapes every character that isn't [A-Za-z0-9-] as "_XX" (lowercase
// hex of the byte). That makes the function a bijection between
// (host, project) and the resulting slug:
//   - alphanumerics and '-' are preserved verbatim,
//   - '_' is escaped to "_5f",
//   - '/', '.', spaces and everything else are escaped to "_<2-hex>".
//
// Two distinct projects that the OLD single-arg Slug collapsed to the
// same string (e.g. "group/sub/proj" and "group.sub.proj" both became
// "group_sub_proj") now produce different paths. Review finding P1 #7.
func Slug(host, project string) string {
	if host == "" {
		host = DefaultHost
	}
	var b strings.Builder
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
			b.Write([]byte(strconv.FormatInt(int64(r), 16)))
		}
	}
	b.WriteByte('_')
	for _, r := range project {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
			b.Write([]byte(strconv.FormatInt(int64(r), 16)))
		}
	}
	return b.String()
}

// Review is the metadata record for one review (review.json).
type Review struct {
	Project        string `json:"project"`
	Host           string `json:"host"`
	IID            int    `json:"iid"`
	Title          string `json:"title"`
	Author         string `json:"author"`
	SourceBranch   string `json:"source_branch"`
	TargetBranch   string `json:"target_branch"`
	HeadSHA        string `json:"head_sha"`
	BaseSHA        string `json:"base_sha"`
	StartSHA       string `json:"start_sha"`
	Worktree       string `json:"worktree"`
	RepoPath       string `json:"repo_path"`
	SourceProject  string `json:"source_project,omitempty"` // full path; set for fork MRs
	State          string `json:"state"`                    // reviewing | published
	SummaryComment string `json:"summary_comment,omitempty"`
	// SummaryDiscussionID is the remote discussion that carries the published
	// summary note (P2 #7): set once by the publisher, it makes summary
	// publication idempotent across re-runs. Empty = not yet published.
	SummaryDiscussionID string   `json:"summary_discussion_id,omitempty"`
	Description         string   `json:"description,omitempty"`    // MR description (background re-run needs it)
	ReviewedFiles       []string `json:"reviewed_files,omitempty"` // manual review progress (per new path)
	ReviewedHunks       []string `json:"reviewed_hunks,omitempty"` // "path|newStart" per hunk
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
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

// marshalFindings renders the findings set as findings.jsonl bytes.
func marshalFindings(fs []Finding) ([]byte, error) {
	var b strings.Builder
	for _, fd := range fs {
		j, err := json.Marshal(fd)
		if err != nil {
			return nil, err
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

// SaveFindings writes findings.jsonl atomically.
//
// NOTE: this is a whole-file write of the fs argument. Callers that read
// findings, change one, and save the result must use UpdateFindings
// instead — a read outside the lock followed by SaveFindings can clobber
// another writer's records (the AI worker vs. the interactive reviewer).
func SaveFindings(dir string, fs []Finding) error {
	b, err := marshalFindings(fs)
	if err != nil {
		return err
	}
	return atomicWrite(FindingsPath(dir), b)
}

// UpdateFindings runs fn as an atomic read-modify-write transaction on
// findings.jsonl: the advisory lock is held across the load, the mutator,
// and the save, so no other writer (the background AI worker, another
// mr-review process, or another goroutine in this process) can slip a
// whole-file write between this call's read and its write.
//
// fn receives the freshly loaded, validated findings and returns the set
// to persist (returning nil persists an empty file, matching
// SaveFindings(dir, nil)). The mutator runs synchronously and must not
// itself call SaveFindings/UpdateFindings on the same dir — the lock is
// not re-entrant and the second acquisition would time out.
//
// The mutator's output is re-validated fail-closed (same gate as
// LoadFindings): an invalid record aborts the transaction and nothing is
// written. An fn error also aborts with no write.
func UpdateFindings(dir string, fn func(fs []Finding) ([]Finding, error)) error {
	return withFileLock(FindingsPath(dir), func() error {
		fs, err := LoadFindings(dir)
		if err != nil {
			return fmt.Errorf("update findings: %w", err)
		}
		out, err := fn(fs)
		if err != nil {
			return err
		}
		for i := range out {
			if err := out[i].Validate(); err != nil {
				return fmt.Errorf("update findings: record %d (%s): %w", i, out[i].ID, err)
			}
			if out[i].ID == "" {
				return fmt.Errorf("update findings: record %d: missing id", i)
			}
		}
		b, err := marshalFindings(out)
		if err != nil {
			return err
		}
		return writeAtomicLocked(FindingsPath(dir), b)
	})
}

// FindingsPath returns the findings.jsonl path.
func FindingsPath(dir string) string { return filepath.Join(dir, "findings.jsonl") }

// RawFindingsPath is where the agent is told to write its raw output.
func RawFindingsPath(dir string) string { return filepath.Join(dir, "findings.raw.jsonl") }

// withFileLock runs fn while holding the advisory lock for path
// ("<path>.lock"). This is the single funnel for lock acquisition in the
// store, so every state-file write shares one locking protocol.
//
// Locking (review finding P1 #8): atomic rename alone does NOT prevent
// two processes from racing to update the same file. In mr-review, the
// AI worker and the interactive reviewer can both write findings.jsonl
// concurrently; without locking they would corrupt each other's writes.
// We use an advisory flock(2) on "<path>.lock" with a short timeout —
// if another process holds it, we fail with an actionable error rather
// than block indefinitely (the lock holder should be quick, and a
// stuck lock points to a real bug to investigate).
//
// In-process: flock/OFD locks are held per open file description, and
// each withFileLock call opens its own fd — so two goroutines in this
// process contending for the same path serialize through the same poll
// loop as two processes. The lock is NOT re-entrant.
func withFileLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("acquire lock for %s: %w", path, err)
	}
	defer lockFile.Close()
	// 5s timeout: the lock holder should finish quickly; if it doesn't,
	// either it's stuck or it died (lockfile leaked). Either way, failing
	// here is safer than blocking indefinitely.
	if err := flockTry(lockFile, 5*time.Second); err != nil {
		return fmt.Errorf("lock %s: %w", lockPath, err)
	}
	defer flockUnlock(lockFile)
	return fn()
}

// atomicWrite writes data to path atomically: advisory lock, then the
// tmp+fsync+rename protocol below. This is the single funnel for any
// state-file write in the store, so callers cannot accidentally bypass
// the locking protocol.
func atomicWrite(path string, data []byte) error {
	return withFileLock(path, func() error { return writeAtomicLocked(path, data) })
}

// writeAtomicLocked writes data to path with the temp-file + rename
// protocol. The caller MUST hold the advisory lock for path (see
// withFileLock) — alone it protects file integrity only, not read-
// modify-write transactions.
//
// Temp filename uniqueness: rename() races are avoided by giving the
// temp file a per-process, per-call name ("<path>.tmp.<pid>.<nanos>"),
// so two concurrent writers cannot both rename the same temp into
// place — whichever wins, the other has nothing to rename.
//
// fsync ordering: the file is fsync'd BEFORE the rename so the data is
// on disk before the directory entry changes; the directory is fsync'd
// after the rename so the rename itself is durable.
func writeAtomicLocked(path string, data []byte) error {
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	// fsync the directory entry so the rename is durable (matters on
	// filesystems where the rename is journaled separately from file
	// data — typical for ext4/xfs, also for journaled APFS).
	if d, derr := os.Open(filepath.Dir(path)); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// flockTry wraps fcntl(F_OFD_SETLK) with a polling fallback so this
// works on macOS (no OFD locks) without changing the contract. Pure
// macOS supports flock(LOCK_EX | LOCK_NB); we use that on darwin and
// fcntl(F_OFD_SETLK) on linux. Either way the contract is "wait up
// to d for the lock, return nil on acquisition, error on contention".
func flockTry(f *os.File, d time.Duration) error {
	return doFlockTry(f, d)
}

func flockUnlock(f *os.File) error {
	return doFlockUnlock(f)
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
