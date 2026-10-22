// Package worktree manages isolated detached-HEAD review worktrees under
// $MR_REVIEW_HOME/worktrees. It never touches the teammate's branch, never
// commits or pushes, and never destroys a dirty worktree.
package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mr-review/internal/store"
)

// Manager operates on one main repository.
type Manager struct {
	RepoPath string
	// Log, when set, receives every repo-level git command before execution
	// ("git fetch origin +refs/...") — used for progress display.
	Log func(cmd string)
}

// New builds a Manager for the repo containing dir (git rev-parse --show-toplevel).
func New(dir string) (*Manager, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not inside a git repository: %w", err)
	}
	return &Manager{RepoPath: strings.TrimSpace(out)}, nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// runGit echoes the command (when Log is set) and runs it in RepoPath.
func (m *Manager) runGit(args ...string) (string, error) {
	if m.Log != nil {
		m.Log("git " + strings.Join(args, " "))
	}
	return git(m.RepoPath, args...)
}

// Path returns the worktree path for a review at a given head SHA.
func (m *Manager) Path(r *store.Review) string {
	short := r.HeadSHA
	if len(short) > 8 {
		short = short[:8]
	}
	return filepath.Join(store.WorktreeRoot(),
		fmt.Sprintf("%s-mr%d-%s", store.Slug(r.Host, r.Project), r.IID, short))
}

// head returns the worktree's checked-out commit ("" when missing).
func (m *Manager) head(wt string) string {
	out, err := git(wt, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// dirty reports uncommitted changes in the worktree.
func (m *Manager) dirty(wt string) bool {
	out, err := git(wt, "status", "--porcelain")
	if err != nil {
		return true // treat unknown as dirty (fail-safe)
	}
	return strings.TrimSpace(out) != ""
}

// fetchMRRef fetches the MR head ref into a local tracking ref.
func (m *Manager) fetchMRRef(r *store.Review) error {
	refspec := fmt.Sprintf("+refs/merge-requests/%d/head:refs/mr-review/%d", r.IID, r.IID)
	if _, err := m.runGit("fetch", "--no-write-fetch-head", "origin", refspec); err != nil {
		// Fork MRs: the ref lives on the source project, not origin.
		if r.SourceProject != "" {
			src := fmt.Sprintf("https://%s/%s.git", r.Host, r.SourceProject)
			if _, err2 := m.runGit("fetch", "--no-write-fetch-head", src, refspec); err2 != nil {
				return fmt.Errorf("fetch MR ref from origin and source project both failed: %v / %v", err, err2)
			}
			return nil
		}
		return fmt.Errorf("fetch MR ref: %w", err)
	}
	return nil
}

// hasCommit verifies the object exists locally.
func (m *Manager) hasCommit(sha string) bool {
	_, err := m.runGit("cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// Ensure returns a ready worktree path pinned to review.HeadSHA.
//
// Rules:
//   - missing            -> fetch + create (detached)
//   - exists at same SHA -> reuse (error when dirty)
//   - exists at other SHA-> remove + recreate (error when dirty; force-pushed MR)
func (m *Manager) Ensure(r *store.Review) (string, error) {
	wt := r.Worktree
	if wt == "" {
		wt = m.Path(r)
		r.Worktree = wt
	}
	if err := os.MkdirAll(store.WorktreeRoot(), 0o755); err != nil {
		return "", err
	}

	switch head := m.head(wt); {
	case wt != "" && head == r.HeadSHA:
		if m.dirty(wt) {
			return "", fmt.Errorf("review worktree %s has local changes; resolve or delete it first", wt)
		}
		return wt, nil // reuse
	case head != "" && head != r.HeadSHA:
		if m.dirty(wt) {
			return "", fmt.Errorf("MR moved to %s but old worktree %s is dirty; resolve or delete it first", shortSHA(r.HeadSHA), wt)
		}
		if _, err := m.runGit("worktree", "remove", "--force", wt); err != nil {
			// fall through to plain directory removal below
			_ = os.RemoveAll(wt)
		}
	}

	if !m.hasCommit(r.HeadSHA) {
		if err := m.fetchMRRef(r); err != nil {
			return "", err
		}
		if !m.hasCommit(r.HeadSHA) {
			return "", fmt.Errorf("MR head %s not found after fetch (force-push? wrong project?)", shortSHA(r.HeadSHA))
		}
	}
	if _, err := m.runGit("worktree", "add", "--detach", wt, r.HeadSHA); err != nil {
		// stale worktree metadata (dir deleted manually): prune and retry once
		_, _ = m.runGit("worktree", "prune")
		if _, err2 := m.runGit("worktree", "add", "--detach", wt, r.HeadSHA); err2 != nil {
			return "", fmt.Errorf("worktree add: %w", err2)
		}
	}
	if got := m.head(wt); got != r.HeadSHA {
		return "", fmt.Errorf("worktree HEAD %s != reviewed SHA %s", shortSHA(got), shortSHA(r.HeadSHA))
	}
	return wt, nil
}

// Diff writes the unified diff base..head into destFile.
func (m *Manager) Diff(wt, base, head, destFile string) error {
	if m.Log != nil {
		m.Log(fmt.Sprintf("git diff --no-color %s..%s > %s", shortSHA(base), shortSHA(head), filepath.Base(destFile)))
	}
	out, err := git(wt, "diff", "--no-color", base+".."+head)
	if err != nil {
		return err
	}
	return os.WriteFile(destFile, []byte(out), 0o600)
}

// Cleanup removes the review worktree (never a dirty one unless force).
func (m *Manager) Cleanup(wt string, force bool) error {
	if !force && m.dirty(wt) {
		return fmt.Errorf("worktree %s is dirty; refusing to remove without --force", wt)
	}
	if _, err := m.runGit("worktree", "remove", "--force", wt); err != nil {
		if err := os.RemoveAll(wt); err != nil {
			return err
		}
	}
	_, _ = m.runGit("worktree", "prune")
	return nil
}

func shortSHA(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
