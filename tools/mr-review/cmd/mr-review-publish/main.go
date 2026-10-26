// cmd/mr-review-publish — the ONLY component with GitLab write capability.
// Run by the human (CLI or the TUI's P key); never by the AI reviewer.
//
// Cobra/viper CLI:
//
//	mr-review-publish <review-dir> [--dry-run] [--yes] [--summary] [--summary-file F]
//	mr-review-publish approve <review-dir> [--yes]
package main

import (
	"fmt"
	"os"

	"mr-review/internal/publish"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mr-review-publish <review-dir>",
		Short: "post human-approved review comments to GitLab (human-only)",
		Long: `Only findings approved in the TUI (or via mr-review set) are posted.
The MR head must still match the reviewed SHA; otherwise run
"mr-review refresh <dir>" first. Inline comments are de-duplicated against
existing discussions before posting; each result is recorded in
publish-log.jsonl.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := publish.Options{
				Dir:         args[0],
				DryRun:      viper.GetBool("dry_run"),
				Yes:         viper.GetBool("yes"),
				Summary:     viper.GetBool("summary") || viper.GetString("summary_file") != "",
				SummaryFile: viper.GetString("summary_file"),
			}
			res, err := publish.Run(opts)
			if err != nil {
				return err
			}
			fmt.Printf("\npublished: %d, skipped: %d, failed: %d\n",
				len(res.Published), len(res.Skipped), len(res.Failed))
			for _, u := range res.Unanchored {
				fmt.Println("  UNANCHORED:", u, "— GitLab dropped the inline position; note edited to carry the file:line location as a general comment")
			}
			for _, s := range res.Skipped {
				fmt.Println("  skipped:", s)
			}
			for _, f := range res.Failed {
				fmt.Println("  FAILED :", f)
			}
			if len(res.Failed) > 0 {
				fmt.Println("\npartial publication — rerun the same command to retry only the failures.")
				os.Exit(res.ExitCode())
			}
			return nil
		},
	}
	root.PersistentFlags().Bool("yes", false, "skip the interactive confirmation (for scripts/CI)")
	root.Flags().Bool("dry-run", false, "show exactly what would be posted; change nothing")
	root.Flags().Bool("summary", false, "also post the general review summary note")
	root.Flags().String("summary-file", "", "post the contents of this file as the summary note")
	_ = viper.BindPFlag("yes", root.PersistentFlags().Lookup("yes"))
	_ = viper.BindPFlag("dry_run", root.Flags().Lookup("dry-run"))
	_ = viper.BindPFlag("summary", root.Flags().Lookup("summary"))
	_ = viper.BindPFlag("summary_file", root.Flags().Lookup("summary-file"))

	approve := &cobra.Command{
		Use:   "approve <review-dir>",
		Short: "formally approve the MR (separate, explicit action)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return publish.Approve(args[0], viper.GetBool("yes"))
		},
	}
	root.AddCommand(approve)
	return root
}
