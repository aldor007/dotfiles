package main

import (
	"fmt"
	"os"

	"mr-review/internal/export"
	"mr-review/internal/store"

	"github.com/spf13/cobra"
)

// newExportCmd writes review data to JSON or Markdown. Export is read-only:
// it never publishes, never changes review state and never calls GitLab.
func newExportCmd() *cobra.Command {
	var (
		format   string
		scope    string
		out      string
		force    bool
		finding  string
		noEvid   bool
		noComm   bool
		rejected bool
		progress bool
	)
	cmd := &cobra.Command{
		Use:   "export <MR|review-dir>",
		Short: "export findings/comments to JSON or Markdown (local only, never publishes)",
		Long: `Writes a versioned snapshot of the review to a file or stdout (-).

Scopes: all | approved | pending | rejected | published | stale |
selected (--finding ID) | manual | report (findings + comments + progress).

Safety: export never publishes comments, never approves anything and never
talks to GitLab. Existing files are never overwritten without --force.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveReviewDir(args[0])
			if err != nil {
				return err
			}
			if !export.ValidScope(export.Scope(scope)) {
				return fmt.Errorf("invalid scope %q (all|approved|pending|rejected|published|stale|selected|manual|report)", scope)
			}
			if format != export.FormatJSON && format != export.FormatMarkdown {
				return fmt.Errorf("invalid format %q (json|markdown)", format)
			}
			opts := export.Options{
				Dir:             dir,
				Scope:           export.Scope(scope),
				Format:          format,
				Out:             out,
				Force:           force,
				FindingID:       finding,
				IncludeEvidence: !noEvid,
				IncludeComments: !noComm,
				IncludeRejected: rejected,
				IncludeProgress: progress,
			}
			// default destination from the review when not stdout
			if out != "-" && out == "" {
				rev, err := store.LoadReview(dir)
				if err != nil {
					return err
				}
				opts.Out = export.DefaultFilename(rev, format, export.Scope(scope))
			}
			res, err := export.Run(opts)
			if err != nil {
				return err
			}
			if res.Path == "" {
				fmt.Fprintf(os.Stderr, "exported %d finding(s), %d comment(s) to stdout\n",
					res.Findings, res.Comments)
			} else {
				fmt.Printf("exported %d finding(s), %d comment(s) → %s\n",
					res.Findings, res.Comments, res.Path)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", export.FormatJSON, "output format: json | markdown")
	cmd.Flags().StringVar(&scope, "scope", string(export.ScopeAll), "what to export (see --help)")
	cmd.Flags().StringVarP(&out, "out", "o", "", "destination file (- for stdout; default mr-<iid>-review.<ext>)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing destination")
	cmd.Flags().StringVar(&finding, "finding", "", "finding ID (required by --scope selected)")
	cmd.Flags().BoolVar(&noEvid, "no-evidence", false, "omit evidence blocks")
	cmd.Flags().BoolVar(&noComm, "no-comments", false, "omit manual comments")
	cmd.Flags().BoolVar(&rejected, "rejected", false, "include rejected findings in all/report scopes")
	cmd.Flags().BoolVar(&progress, "progress", false, "include manual review progress")
	return cmd
}
