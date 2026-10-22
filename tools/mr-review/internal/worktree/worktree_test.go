package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mr-review/internal/store"
)

// initRepo creates a real git repo with two branches and returns its path
// plus the SHAs of base and feature commits.
func initRepo(t *testing.T) (repo, base, feat string) {
	t.Helper()
	repo = t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "master", ".")
	_ = os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644)
	run("add", ".")
	run("commit", "-qm", "base")
	base = run("rev-parse", "HEAD")
	run("checkout", "-qb", "feature")
	_ = os.WriteFile(filepath.Join(repo, "b.go"), []byte("package b\n"), 0o644)
	run("add", ".")
	run("commit", "-qm", "feat")
	feat = run("rev-parse", "HEAD")
	return repo, base, feat
}

func reviewFor(repo string, iid int, sha string) *store.Review {
	return &store.Review{
		Project: "grp/proj", IID: iid, HeadSHA: sha,
		SourceBranch: "feature", TargetBranch: "master",
		RepoPath: repo, State: "reviewing",
	}
}

func TestEnsureCreatesDetachedAndReuses(t *testing.T) {
	t.Setenv(store.EnvHome, t.TempDir())
	repo, base, feat := initRepo(t)
	m, err := New(repo)
	if err != nil {
		t.Fatal(err)
	}
	r := reviewFor(repo, 1, feat)
	wt, err := m.Ensure(r)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if m.head(wt) != feat {
		t.Fatal("worktree not at MR head")
	}
	// detached HEAD?
	out, err := git(wt, "symbolic-ref", "-q", "HEAD")
	if err == nil && strings.TrimSpace(out) != "" {
		t.Fatalf("expected detached HEAD, got branch %s", out)
	}
	// reuse
	wt2, err := m.Ensure(r)
	if err != nil || wt2 != wt {
		t.Fatalf("reuse: %v %s", err, wt2)
	}
	_ = base
}

func TestEnsureRecreatesOnForcePush(t *testing.T) {
	t.Setenv(store.EnvHome, t.TempDir())
	repo, _, feat := initRepo(t)
	m, _ := New(repo)
	r := reviewFor(repo, 2, feat)
	wt1, err := m.Ensure(r)
	if err != nil {
		t.Fatal(err)
	}
	// simulate a force-push: review moves to the base commit. With the
	// worktree pinned in the review record, Ensure recreates IN PLACE.
	r.HeadSHA = mustBaseSHA(t, repo)
	wt2, err := m.Ensure(r)
	if err != nil {
		t.Fatalf("recreate: %v", err)
	}
	if m.head(wt2) != r.HeadSHA {
		t.Fatal("recreated worktree not at new SHA")
	}
	if wt2 != wt1 {
		t.Fatal("pinned worktree should be recreated in place")
	}
	// with the pin cleared (fresh start), the new SHA maps to its own path
	r.Worktree = ""
	wt3, err := m.Ensure(r)
	if err != nil {
		t.Fatalf("fresh path: %v", err)
	}
	if wt3 == wt1 {
		t.Fatal("expected a new worktree path for the new SHA")
	}
	if m.head(wt3) != r.HeadSHA {
		t.Fatal("fresh worktree not at new SHA")
	}
}

func mustBaseSHA(t *testing.T, repo string) string {
	t.Helper()
	out, err := git(repo, "rev-parse", "master")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

func TestEnsureRefusesDirtyWorktree(t *testing.T) {
	t.Setenv(store.EnvHome, t.TempDir())
	repo, _, feat := initRepo(t)
	m, _ := New(repo)
	r := reviewFor(repo, 3, feat)
	wt, err := m.Ensure(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "junk.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Ensure(r); err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("expected dirty refusal, got %v", err)
	}
	// cleanup without force must also refuse
	if err := m.Cleanup(wt, false); err == nil {
		t.Fatal("cleanup must refuse dirty worktrees")
	}
	if err := m.Cleanup(wt, true); err != nil {
		t.Fatalf("force cleanup: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("worktree still exists after force cleanup")
	}
}

func TestDiffWritesPatch(t *testing.T) {
	t.Setenv(store.EnvHome, t.TempDir())
	repo, base, feat := initRepo(t)
	m, _ := New(repo)
	r := reviewFor(repo, 4, feat)
	wt, err := m.Ensure(r)
	if err != nil {
		t.Fatal(err)
	}
	diffPath := filepath.Join(t.TempDir(), "diff.patch")
	if err := m.Diff(wt, base, feat, diffPath); err != nil {
		t.Fatalf("diff: %v", err)
	}
	b, err := os.ReadFile(diffPath)
	if err != nil || !strings.Contains(string(b), "b.go") {
		t.Fatalf("diff content: %v %s", err, b)
	}
}

func TestEnsureMissingCommitFailsClosed(t *testing.T) {
	t.Setenv(store.EnvHome, t.TempDir())
	repo, _, _ := initRepo(t)
	m, _ := New(repo)
	r := reviewFor(repo, 5, "0123456789abcdef0123456789abcdef01234567")
	if _, err := m.Ensure(r); err == nil {
		t.Fatal("expected failure for unknown SHA (fetch would fail in stub-less env)")
	}
}
