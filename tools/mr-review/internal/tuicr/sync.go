// sync.go — staging human-approved AI findings into a tuicr review session.
//
// Rules (spec §10):
//   - only findings with decision == approved are staged, never pending/rejected,
//   - each finding is staged at most once (mapping persisted; duplicates skipped),
//   - the comment is read back and verified after insertion,
//   - human edits in tuicr always win: sync reads state, never rewrites text,
//   - stale findings (lines moved / revision changed) are never staged,
//   - no remote GitLab write happens anywhere in this file.
package tuicr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mr-review/internal/store"
)

// Mapping records that a finding was staged into a tuicr session.
type Mapping struct {
	FindingID   string `json:"finding_id"`
	Session     string `json:"session"`
	CommentID   string `json:"tuicr_comment_id"`
	StagedAt    string `json:"staged_at"`
	ContentHash string `json:"content_hash"` // staged text hash (detects human edits)
	Location    string `json:"location"`     // file:line (tuicr's view)
}

// MappingStore persists finding→tuicr-comment mappings in the review dir.
type MappingStore struct {
	dir  string
	mu   sync.Mutex
	data map[string]Mapping // finding id → mapping
}

// MappingPath returns the tuicr-map.json path for a review dir.
func MappingPath(dir string) string { return filepath.Join(dir, "tuicr-map.json") }

// LoadMappings reads the mapping store (empty when absent).
func LoadMappings(dir string) (*MappingStore, error) {
	m := &MappingStore{dir: dir, data: map[string]Mapping{}}
	b, err := os.ReadFile(MappingPath(dir))
	if err == nil && len(b) > 0 {
		if json.Unmarshal(b, &m.data) != nil {
			return nil, fmt.Errorf("corrupt %s", MappingPath(dir))
		}
	}
	return m, nil
}

func (m *MappingStore) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, err := json.MarshalIndent(m.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := MappingPath(m.dir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, MappingPath(m.dir))
}

func (m *MappingStore) Get(findingID string) (Mapping, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[findingID]
	return v, ok
}

func (m *MappingStore) put(v Mapping) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[v.FindingID] = v
}

// StageOptions controls one staging run.
type StageOptions struct {
	Dir      string // review dir (mapping store, findings)
	RepoPath string // worktree/checkout for `tuicr review list --repo`
	Project  string // GitLab project path (PR session matching)
	IID      int
	Username string // author stamped on staged comments
	// Host and Revision are optional exact-match expectations (review
	// finding P1-2): when set, a session that RECORDS a differing host or
	// reviewed revision is rejected instead of staged into. tuicr's current
	// `review list` output records neither, so they stay empty today; if a
	// future tuicr version publishes them, the check is enforced
	// automatically. Never wired: Project/IID matching is ALWAYS exact.
	Host     string
	Revision string
}

// StageResult reports one staging run honestly.
type StageResult struct {
	Staged  []string // finding ids inserted now
	Skipped []string // "id: reason"
	Failed  []string // "id: error"
}

// StageApproved transfers approved, unpublished, non-stale findings into the
// resolved tuicr session. Idempotent: already-mapped findings are verified
// (still present) and skipped.
func StageApproved(ctx context.Context, opts StageOptions, cli *CLI) (*StageResult, error) {
	res := &StageResult{}
	findings, err := store.LoadFindings(opts.Dir)
	if err != nil {
		return nil, fmt.Errorf("load findings: %w", err)
	}
	sessionSlug, err := cli.ResolveSessionSmartForMR(ctx, opts.RepoPath, MRIdentity{
		Project: opts.Project, IID: opts.IID, Host: opts.Host, Revision: opts.Revision,
	})
	if err != nil {
		return nil, err
	}

	mappings, err := LoadMappings(opts.Dir)
	if err != nil {
		return nil, err
	}
	// prune mappings pointing at a session that no longer exists (cleared
	// or rebuilt) — the staged comments are gone with it, so the findings
	// become stageable again into the current session
	existing, err := cli.Comments(ctx, sessionSlug, opts.RepoPath)
	if err != nil {
		// the session itself may be gone: drop stale mappings and retry once
		_ = os.Remove(MappingPath(opts.Dir))
		mappings, err = LoadMappings(opts.Dir)
		if err != nil {
			return nil, err
		}
		existing, err = cli.Comments(ctx, sessionSlug, opts.RepoPath)
		if err != nil {
			return nil, err
		}
	}
	for id, mp := range mappings.data {
		if mp.Session != sessionSlug {
			mappings.mu.Lock()
			delete(mappings.data, id)
			mappings.mu.Unlock()
		}
	}
	_ = mappings.Save()
	byID := map[string]Comment{}
	for _, c := range existing {
		byID[c.ID] = c
	}

	for i := range findings {
		f := &findings[i]
		switch {
		case f.Decision != "approved":
			continue // silently: pending/rejected are simply not eligible
		case f.Stale:
			res.Skipped = append(res.Skipped, f.ID+": stale (revision changed — refresh first)")
			continue
		case f.Publication != nil:
			res.Skipped = append(res.Skipped, f.ID+": already published via mr-review")
			continue
		}
		if mapped, ok := mappings.Get(f.ID); ok {
			if mapped.Session != sessionSlug {
				// mapped into a since-deleted session: drop and re-stage
				mappings.mu.Lock()
				delete(mappings.data, f.ID)
				mappings.mu.Unlock()
			}
			if _, alive := byID[mapped.CommentID]; alive {
				res.Skipped = append(res.Skipped, f.ID+": already staged ("+mapped.CommentID+")")
			} else {
				res.Skipped = append(res.Skipped, f.ID+": staged comment was deleted in tuicr — delete the mapping or re-stage deliberately")
			}
			continue
		}

		in := CommentInput{
			Content:  f.EffectiveComment(),
			Type:     "issue",
			File:     f.File,
			Line:     f.Line,
			Side:     f.Side,
			Username: opts.Username,
		}
		if f.LineEnd > f.Line {
			in.StartLine, in.EndLine = f.Line, f.LineEnd
		}
		stored, err := cli.AddComment(ctx, sessionSlug, opts.RepoPath, in)
		if err != nil {
			if strings.Contains(err.Error(), "session does not contain file") {
				// tuicr's own PR diff fetch missed this file (observed for
				// charts/ templates): it cannot inline the comment there.
				// The finding stays publishable via mr-review-publish, which
				// posts it inline on GitLab directly.
				res.Skipped = append(res.Skipped, f.ID+": file not in tuicr's diff — "+
					f.File+" cannot be inlined by tuicr; "+
					"mr-review publish will still post it inline on GitLab")
			} else {
				res.Failed = append(res.Failed, f.ID+": "+err.Error())
			}
			continue
		}
		// read-back verification: location + text must match what we sent
		if stored.Path != f.File || stored.StartLine != f.Line || !contentMatches(stored.Content, f.EffectiveComment()) {
			_ = cli.DeleteComment(ctx, sessionSlug, stored.ID, opts.RepoPath) // undo: never keep a mismatched insertion
			res.Failed = append(res.Failed, f.ID+": verification failed after insert (location/text mismatch)")
			continue
		}
		mappings.put(Mapping{
			FindingID:   f.ID,
			Session:     sessionSlug,
			CommentID:   stored.ID,
			StagedAt:    store.Now(),
			ContentHash: hashContent(stored.Content),
			Location:    stored.Location,
		})
		res.Staged = append(res.Staged, f.ID)
	}
	if err := mappings.Save(); err != nil {
		return res, fmt.Errorf("saving mapping: %w", err)
	}
	return res, nil
}

// SyncState reads the session back and reports human interaction with staged
// comments: edited (content hash changed) or deleted (id gone). It NEVER
// writes anything — the tuicr text is authoritative once staged.
func SyncState(ctx context.Context, opts StageOptions, cli *CLI) (edited []string, deleted []string, err error) {
	mappings, err := LoadMappings(opts.Dir)
	if err != nil {
		return nil, nil, err
	}
	sessionSlug, err := cli.ResolveSessionSmartForMR(ctx, opts.RepoPath, MRIdentity{
		Project: opts.Project, IID: opts.IID, Host: opts.Host, Revision: opts.Revision,
	})
	if err != nil {
		return nil, nil, err
	}
	existing, err := cli.Comments(ctx, sessionSlug, opts.RepoPath)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]Comment{}
	for _, c := range existing {
		byID[c.ID] = c
	}
	for _, m := range mappings.data {
		c, ok := byID[m.CommentID]
		if !ok {
			deleted = append(deleted, m.FindingID)
			continue
		}
		if hashContent(c.Content) != m.ContentHash {
			edited = append(edited, m.FindingID) // human edited in tuicr: their text wins
		}
	}
	return edited, deleted, nil
}

func contentMatches(a, b string) bool {
	return normalizeText(a) == normalizeText(b)
}

func normalizeText(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\r' || r == '\n' {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func hashContent(s string) string {
	sum := sha256.Sum256([]byte(normalizeText(s)))
	return hex.EncodeToString(sum[:8])
}

// StagedMapping is the exported view of a mapping (for export/report).
type StagedMapping = Mapping

var _ = time.Now
