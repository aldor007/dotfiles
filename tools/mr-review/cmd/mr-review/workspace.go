// workspace.go — Hedr workspace orchestration: prepare the review session,
// spawn the background AI review, and open the hybrid workspace — tuicr for
// manual review next to the Go AI triage cockpit. Control returns to the
// user immediately; nothing here waits for AI analysis.
package main

import (
	"fmt"
	"strings"

	"mr-review/internal/gitlab"
	"mr-review/internal/store"
	"mr-review/internal/tui"

	"github.com/spf13/cobra"
)

func newWorkspaceCmd() *cobra.Command {
	var noTuicr, noCockpit bool
	cmd := &cobra.Command{
		Use:   "workspace <MR-URL|IID|review-dir>",
		Short: "open the hybrid review workspace: tuicr (manual) + AI cockpit + background AI review",
		Long: `Prepares the review session (MR fetch, pinned worktree, diff), starts the
Kimchi/pi AI review detached, and opens the two review panes via Hedr:

  right pane: tuicr  — continuous diff, manual comments (immediately usable)
  below pane: mr-review tui — AI findings triage cockpit (fills when AI done)

Manual review never waits for AI analysis. Requires an open MR URL/IID or an
existing review dir. Without Hedr the two launch commands are printed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, dir, err := resolveRef(args[0])
			if err != nil {
				return err
			}
			rev, err := store.LoadReview(dir)
			if err != nil {
				// not prepared yet: fetch MR + worktree + diff now (fast, no AI)
				rev, err = prepareReviewRef(ref)
				if err != nil {
					return err
				}
				dir = store.ReviewDir(rev.Host, rev.Project, rev.IID)
			}
			if st := store.ReadAgentState(dir); st == nil || st.Status != store.AgentRunning {
				if err := store.SpawnReviewBG(dir, rev.HeadSHA); err != nil {
					logWarnf("background AI review could not start: %v", err)
				} else {
					fmt.Printf("AI review running in background (head %s)\n", short(rev.HeadSHA))
				}
			} else {
				fmt.Println("background AI review already running — not starting a duplicate")
			}

			fmt.Printf("AI review running in background — opening the hybrid workspace (cockpit + embedded tuicr)\n")
			fmt.Println("Ctrl+G toggles focus · W closes the tuicr pane · q quits")
			if noCockpit || noTuicr {
				fmt.Println("note: the workspace always shows both panes; --no-tuicr/--no-cockpit ignored")
			}
			return tui.RunWorkspace(dir)
		},
	}
	cmd.Flags().Bool("no-tuicr", false, "kept for compatibility (no effect)")
	cmd.Flags().Bool("no-cockpit", false, "kept for compatibility (no effect)")
	return cmd
}

func prepareReviewRef(ref *gitlab.MRRef) (*store.Review, error) {
	_, rev, err := prepareReview(ref)
	return rev, err
}

// inHedr reports whether this process runs inside a Hedr pane.

// extractPaneID parses the pane id from a herdr pane split output.
func extractPaneID(out string) string {
	const key = `"pane_id":"`
	i := strings.Index(out, key)
	if i < 0 {
		return ""
	}
	rest := out[i+len(key):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return ""
	}
	return rest[:j]
}
