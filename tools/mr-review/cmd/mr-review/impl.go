package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mr-review/internal/gitlab"
	"mr-review/internal/store"
	"mr-review/internal/worktree"
)

// runList prints MRs worth reviewing (label-filtered when labels are set).
func runList(labels []string) error { return listMRs(labels) }

// runStatus walks all local reviews (implementation in status.go).
func runStatus() error { return cmdStatus() }

// runDoctor checks the toolchain (implementation in doctor.go).
func runDoctor() error { return cmdDoctor() }

// runSet records a triage decision.
func runSet(dir, id, decision string) error {
	switch decision {
	case "approved", "rejected", "pending":
	default:
		return fmt.Errorf("bad decision %q (approved|rejected|pending)", decision)
	}
	fs, err := store.LoadFindings(dir)
	if err != nil {
		return err
	}
	found := false
	for i := range fs {
		if fs[i].ID == id {
			fs[i].Decision = decision
			found = true
		}
	}
	if !found {
		return fmt.Errorf("finding %s not found in %s", id, dir)
	}
	return store.SaveFindings(dir, fs)
}

// runDiscuss asks the agent one question about a finding.
func runDiscuss(dir, id, question string) error {
	ans, err := agentRunner().Discuss(discussCtx(), dir, id, question, discussTimeout())
	if err != nil {
		return err
	}
	fmt.Println(ans)
	return nil
}

// runClean removes review worktrees. Targets: one dir, --all, or --merged
// (MR state checked via the API; failures skip that review with a warning).
func runClean(dir string, all, merged, force bool) error {
	type target struct {
		dir string
		rev *store.Review
	}
	var targets []target

	collect := func(d string) {
		rev, err := store.LoadReview(d)
		if err != nil {
			logWarnf("skip %s: %v", d, err)
			return
		}
		targets = append(targets, target{d, rev})
	}

	switch {
	case dir != "":
		collect(dir)
	case all || merged:
		root := store.Home()
		entries, rerr := os.ReadDir(root)
		if rerr != nil {
			return fmt.Errorf("no reviews found (%v)", rerr)
		}
		for _, p := range entries {
			mrDirs, _ := os.ReadDir(filepath.Join(root, p.Name()))
			for _, m := range mrDirs {
				if m.IsDir() && strings.HasPrefix(m.Name(), "mr") {
					collect(filepath.Join(root, p.Name(), m.Name()))
				}
			}
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("no reviews found")
	}

	gl := gitlab.New()
	removed, kept := 0, 0
	for _, t := range targets {
		if t.rev.Worktree == "" {
			continue // nothing to remove
		}
		if merged {
			gl.Host = t.rev.Host
			mr, err := gl.GetMR(t.rev.Project, t.rev.IID)
			if err != nil {
				logWarnf("skip !%d (%s): cannot check MR state: %v", t.rev.IID, t.rev.Project, err)
				kept++
				continue
			}
			if mr.State == "opened" {
				kept++
				continue // still active: keep the worktree
			}
		}
		m, err := worktree.New(t.rev.RepoPath)
		if err != nil {
			logWarnf("skip !%d: %v", t.rev.IID, err)
			kept++
			continue
		}
		if err := m.Cleanup(t.rev.Worktree, force); err != nil {
			logWarnf("!%d: %v", t.rev.IID, err)
			kept++
			continue
		}
		t.rev.Worktree = ""
		_ = t.rev.Save(t.dir)
		fmt.Printf("✓ removed worktree of !%d (%s)\n", t.rev.IID, t.rev.Project)
		removed++
	}
	fmt.Printf("removed %d worktree(s), kept %d\n", removed, kept)
	return nil
}

func joinRest(rest []string) string {
	return strings.Join(rest, " ")
}

// splitLabels parses a comma-separated label list ("a, b" -> [a b]).
func splitLabels(s string) []string {
	var out []string
	for _, l := range strings.Split(s, ",") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// currentRepo anchors a review in the user's local clone of the project.
func currentRepo() (*worktree.Manager, error) {
	cwd, _ := os.Getwd()
	return worktree.New(cwd)
}
