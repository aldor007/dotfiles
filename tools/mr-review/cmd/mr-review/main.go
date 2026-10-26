// cmd/mr-review — GitLab MR AI review launcher for herdr.
//
// CLI is built on cobra; flags and configuration are managed by viper
// (flags > env MR_REVIEW_* > ~/.config/mr-review.yaml > defaults).
//
// Publishing is intentionally NOT here: see cmd/mr-review-publish.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mr-review/internal/gitlab"
	"mr-review/internal/store"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func main() {
	cobra.OnInitialize(initConfig)
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mr-review [MR-URL|IID]",
		Short: "GitLab MR AI review workflow (herdr-first)",
		Long: `Launch a GitLab MR review from herdr, have the local AI agent
(kimchi/pi) analyze the teammate's changes in an isolated worktree, triage
findings in an interactive TUI, and publish only human-approved comments.

Publishing lives in the separate mr-review-publish binary and always requires
explicit confirmation. The AI reviewer has no write access to GitLab.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			labels := viper.GetStringSlice("label")
			if len(args) == 0 {
				return runPicker(labels)
			}
			return defaultFlow(args[0])
		},
	}
	root.PersistentFlags().StringSliceP("label", "l", nil,
		"filter MRs by label (repeatable or comma-separated; ALL must match)")
	_ = viper.BindPFlag("label", root.PersistentFlags().Lookup("label"))
	root.PersistentFlags().String("log-level", "info", "verbosity: error | warn | info | debug (config: log_level, env: MR_REVIEW_LOG)")
	_ = viper.BindPFlag("log_level", root.PersistentFlags().Lookup("log-level"))
	root.PersistentFlags().Duration("timeout", 25*time.Minute,
		"AI review run timeout (config: timeout, env: MR_REVIEW_TIMEOUT)")
	_ = viper.BindPFlag("timeout", root.PersistentFlags().Lookup("timeout"))
	root.PersistentFlags().Duration("discuss-timeout", 5*time.Minute,
		"per-question agent discussion timeout")
	_ = viper.BindPFlag("discuss_timeout", root.PersistentFlags().Lookup("discuss-timeout"))
	root.PersistentFlags().String("theme", "auto",
		"color theme: auto|default|dracula (config: theme, env: MR_REVIEW_THEME)")
	_ = viper.BindPFlag("theme", root.PersistentFlags().Lookup("theme"))
	root.PersistentFlags().String("icons", "auto",
		"icon set: auto|nerd|ascii (config: icons, env: MR_REVIEW_ICONS)")
	_ = viper.BindPFlag("icons", root.PersistentFlags().Lookup("icons"))

	root.AddCommand(
		newListCmd(), newStartCmd(), newResumeCmd(), newStatusCmd(),
		newDoctorCmd(), newSetCmd(), newOpenCmd(), newDiscussCmd(),
		newRefreshCmd(), newCleanCmd(), newTUICmd(), newFollowCmd(), newExportCmd(),
		newReviewBGCmd(), newWorkspaceCmd(), newContextCmd(),
		newViewCmd("prompt", "the exact prompt sent to the AI reviewer"),
		newViewCmd("log", "the raw agent output (what it did and said)"),
	)
	return root
}

// newViewCmd builds the `prompt`/`log` file viewers.
func newViewCmd(name, desc string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <MR|review-dir>",
		Short: desc,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fname := name
			if name == "prompt" {
				fname = "prompt.txt"
			} else if name == "log" {
				fname = "agent.log"
			}
			dir, err := resolveReviewDir(args[0])
			if err != nil {
				return err
			}
			b, err := os.ReadFile(filepath.Join(dir, fname))
			if err != nil {
				return fmt.Errorf("%s not written yet (run a review first)", fname)
			}
			_, err = os.Stdout.Write(b)
			return err
		},
	}
}

// initConfig wires viper: optional ~/.config/mr-review.yaml + MR_REVIEW_* env
// (flags > env > config file > defaults).
func initConfig() {
	home, err := os.UserHomeDir()
	if err == nil {
		viper.SetConfigName("mr-review")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(filepath.Join(home, ".config"))
		viper.SetDefault("timeout", "25m")
		viper.SetDefault("discuss_timeout", "5m")
		viper.SetDefault("log_level", "info")
		viper.SetDefault("theme", "auto")
		viper.SetDefault("icons", "auto")
		if readErr := viper.ReadInConfig(); readErr == nil {
			fmt.Fprintf(os.Stderr, "mr-review: using config %s\n", viper.ConfigFileUsed())
		}
	}
	viper.SetEnvPrefix("MR_REVIEW")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	// flags > env (MR_REVIEW_LOG) > config file > default — set after all
	// sources are registered.
	setLogLevel(parseLevel(viper.GetString("log_level")))
}

// defaultFlow: bare MR URL or numeric IID — start, or resume when state exists.
func defaultFlow(arg string) error {
	ref, dir, err := resolveRef(arg)
	if err != nil {
		return err
	}
	if dir != "" {
		if _, statErr := os.Stat(filepath.Join(dir, "findings.jsonl")); statErr == nil {
			return runTUI(dir)
		}
	}
	return startReview(ref, false)
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "open MRs: reviewer/assignee=@me first, then authored, then the project (or label-filtered)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(viper.GetStringSlice("label"))
		},
	}
}

func newStartCmd() *cobra.Command {
	var noTUI bool
	cmd := &cobra.Command{
		Use:   "start <MR-URL|IID>",
		Short: "fetch the MR, create the review worktree, run the AI review",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, _, err := resolveRef(args[0])
			if err != nil {
				return err
			}
			return startReview(ref, noTUI)
		},
	}
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "skip opening the findings TUI afterwards")
	return cmd
}

func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <MR-URL|IID|review-dir>",
		Short: "open the findings TUI for an existing review (starts one if missing)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, dir, err := resolveRef(args[0])
			if err != nil {
				return err
			}
			if dir != "" {
				return runTUI(dir)
			}
			return startReview(ref, false)
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "all local reviews + decision counts",
		RunE:  func(cmd *cobra.Command, args []string) error { return runStatus() },
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "dependency and auth checks",
		RunE:  func(cmd *cobra.Command, args []string) error { return runDoctor() },
	}
}

func newSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <MR|dir> <finding-id> <approved|rejected|pending>",
		Short: "record a triage decision (scriptable)",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveReviewDir(args[0])
			if err != nil {
				return err
			}
			return runSet(dir, args[1], args[2])
		},
	}
}

func newOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <MR|dir> [finding-id]",
		Short: "open the finding in nvim (worktree, exact line)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) > 1 {
				id = args[1]
			}
			dir, err := resolveReviewDir(args[0])
			if err != nil {
				return err
			}
			return openFindingCLI(dir, id)
		},
	}
}

func newDiscussCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "discuss <MR|dir> <finding-id> <question...>",
		Short: "ask the AI agent about a finding; the answer is recorded on it",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveReviewDir(args[0])
			if err != nil {
				return err
			}
			return runDiscuss(dir, args[1], joinRest(args[2:]))
		},
	}
}

func newRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh <MR|dir>",
		Short: "re-review after the MR moved (human decisions are preserved)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveReviewDir(args[0])
			if err != nil {
				return err
			}
			return refreshReview(dir)
		},
	}
}

func newCleanCmd() *cobra.Command {
	var all, merged, force bool
	cmd := &cobra.Command{
		Use:   "clean [dir]",
		Short: "remove review worktrees (dirty ones are refused without --force)",
		Long: `Removes review worktrees:
  mr-review clean <dir>      the given review's worktree
  mr-review clean --all      every review's worktree
  mr-review clean --merged   worktrees of merged/closed MRs (via API)

Review state (findings, decisions, logs) is always kept — only the
worktree checkout is removed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var dir string
			if len(args) > 0 {
				dir = args[0]
			}
			if dir == "" && !all && !merged {
				return fmt.Errorf("nothing to clean: pass a review dir, --all, or --merged")
			}
			if dir != "" && (all || merged) {
				return fmt.Errorf("pass either a dir or --all/--merged, not both")
			}
			if dir != "" {
				resolved, rerr := resolveReviewDir(dir)
				if rerr != nil {
					return rerr
				}
				dir = resolved
			}
			return runClean(dir, all, merged, force)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "remove every review worktree")
	cmd.Flags().BoolVar(&merged, "merged", false, "remove worktrees of merged/closed MRs")
	cmd.Flags().BoolVar(&force, "force", false, "remove even dirty worktrees")
	return cmd
}

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui <MR|dir>",
		Short: "open the findings TUI",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveReviewDir(args[0])
			if err != nil {
				return err
			}
			return runTUI(dir)
		},
	}
}

// ---- shared helpers --------------------------------------------------------

// resolveRef turns a CLI arg (URL, IID, or review dir) into a resolved ref
// plus the existing review dir ("" when none).
func resolveRef(arg string) (*gitlab.MRRef, string, error) {
	if st, err := os.Stat(arg); err == nil && st.IsDir() {
		rev, lerr := store.LoadReview(arg)
		if lerr == nil {
			return &gitlab.MRRef{Project: rev.Project, IID: rev.IID, Host: rev.Host}, arg, nil
		}
		return nil, "", fmt.Errorf("%s is not a review dir (no valid review.json)", arg)
	}
	cwd, _ := os.Getwd()
	ref, err := gitlab.ParseMRRef(arg, cwd)
	if err != nil {
		return nil, "", err
	}
	return ref, store.ReviewDir(ref.Host, ref.Project, ref.IID), nil
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
