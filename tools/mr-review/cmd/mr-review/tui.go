package main

import (
	"fmt"
	"os"

	"github.com/spf13/viper"

	"mr-review/internal/store"
	"mr-review/internal/tui"
)

// runTUI launches the interactive findings screen for a review dir.
//
// Theme/icon configuration is resolved from viper (flags > env MR_REVIEW_*
// > ~/.config/mr-review.yaml > defaults) immediately before launch. The
// value "auto" (the default) means "do nothing" — preserves byte-identical
// pre-widget output. Any other value is forwarded to tui.SetTheme /
// tui.SetIcons; on error we print a warning to stderr and continue with
// whatever theme is currently active (cosmetics never fail the launch).
func runTUI(dir string) error {
	applyVisualConfig()
	return tui.Run(dir)
}

// applyVisualConfig resolves the theme/icons viper keys and forwards them
// to the tui package. Split out from runTUI so tests can drive it without
// launching the Bubble Tea program. The "auto" sentinel (and the empty
// value viper may surface when nothing is configured) means "do nothing" —
// this is the freeze guarantee: with no flag/env/config the app renders
// byte-identically.
func applyVisualConfig() {
	if name := viper.GetString("theme"); name != "" && name != "auto" {
		if err := tui.SetTheme(name, viper.GetStringMapString("theme_colors")); err != nil {
			fmt.Fprintf(os.Stderr, "mr-review: theme %q not applied: %v\n", name, err)
		}
	}
	if mode := viper.GetString("icons"); mode != "" && mode != "auto" {
		if err := tui.SetIcons(mode); err != nil {
			fmt.Fprintf(os.Stderr, "mr-review: icons %q not applied: %v\n", mode, err)
		}
	}
}

// openFindingCLI is the CLI `open` entry (delegates to the TUI package's
// herdr-aware nvim launcher).
func openFindingCLI(dir, findingID string) error {
	rev, err := store.LoadReview(dir)
	if err != nil {
		return err
	}
	if rev.Worktree == "" {
		return fmt.Errorf("review has no worktree; run mr-review start/resume first")
	}
	file, line := "", 1
	if findingID != "" {
		fs, err := store.LoadFindings(dir)
		if err != nil {
			return err
		}
		for _, f := range fs {
			if f.ID == findingID {
				file, line = f.File, f.Line
				break
			}
		}
		if file == "" {
			return fmt.Errorf("finding %s not found", findingID)
		}
	}
	return tui.OpenInNvim(rev.Worktree, file, line)
}
