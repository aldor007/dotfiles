// bgreview.go — internal worker: runs the AI review synchronously in a
// detached session (spawned by store.SpawnReviewBG), ingests the findings,
// and records the outcome in agent.state. The TUI polls that file, so the
// user reviews the diff while this runs.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"mr-review/internal/agent"
	"mr-review/internal/gitlab"
	"mr-review/internal/store"

	"github.com/spf13/cobra"
)

func newReviewBGCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "review-bg <review-dir>",
		Short:  "run the AI review in this session (internal; spawned detached)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			rev, err := store.LoadReview(dir)
			if err != nil {
				return err
			}
			st := store.ReadAgentState(dir)
			if st == nil {
				st = &store.AgentState{Status: store.AgentRunning}
			}
			// revision binding: never analyze (or ingest) against a different
			// revision than the one the job was started for (spec §9)
			if st.HeadSHA != "" && rev.HeadSHA != "" && st.HeadSHA != rev.HeadSHA {
				err := fmt.Errorf("job pinned to %s but review is now at %s — re-run the review for the new revision",
					st.HeadSHA, rev.HeadSHA)
				st.Status = store.AgentFailed
				st.Error = err.Error()
				st.FinishedAt = store.Now()
				store.WriteAgentState(dir, *st)
				return err
			}
			finish := func(status, errMsg string) {
				st.Status = status
				st.Error = errMsg
				st.FinishedAt = store.Now()
				if fs, lerr := store.LoadFindings(dir); lerr == nil {
					st.Findings = len(fs)
				}
				st.Raw = countLines(store.RawFindingsPath(dir))
				store.WriteAgentState(dir, *st)
			}
			defer func() {
				if st.Status == store.AgentRunning {
					finish(store.AgentDone, "")
				}
			}()

			gl := gitlab.New()
			gl.Host = rev.Host
			var discs []gitlab.Discussion
			if b, rerr := os.ReadFile(filepath.Join(dir, "discussions.json")); rerr == nil {
				_ = json.Unmarshal(b, &discs)
			}
			if err := runAgentReview(gl, dir, rev, rev.Worktree, discs, newSteps(), rev.Description); err != nil {
				finish(store.AgentFailed, err.Error())
				return err
			}
			// re-run over existing findings: reconcile (carry decisions,
			// mark un-re-reported ones stale) instead of blind append
			if prev, lerr := store.LoadFindings(dir); lerr == nil && len(prev) > 0 {
				res, ierr := agent.ReconcileRun(dir)
				if ierr != nil {
					finish(store.AgentFailed, ierr.Error())
					return ierr
				}
				fmt.Printf("reconciled: %d new, %d dropped/stale, %d total\n",
					res.Added, len(res.Dropped), len(prev)+res.Added)
				return nil
			}
			res, ierr := agent.Ingest(dir)
			if ierr != nil {
				finish(store.AgentFailed, ierr.Error())
				return ierr
			}
			fmt.Printf("findings validated: %d added, %d dropped\n", res.Added, len(res.Dropped))
			return nil
		},
	}
}
