package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mr-review/internal/agent"
	"mr-review/internal/gitlab"
	"mr-review/internal/store"

	"github.com/spf13/cobra"
)

// contextWithInterrupt returns a ctx cancelled on Ctrl-C/SIGTERM.
func contextWithInterrupt() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-ch:
			cancel()
		case <-ctx.Done():
		}
		signal.Stop(ch)
	}()
	return ctx, cancel
}

func newFollowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "follow <review-dir>",
		Short: "live console: watch what the AI reviewer is doing (read-only)",
		Long: `Tails the reviewer's kimchi session transcript in real time: tool calls
with full arguments, thinking, the agent's own notes, token burn, and the
findings file as it is written. Read-only — Ctrl-C to detach (the review
keeps running in the background).

Tip: run it in a herdr split pane next to your work:
  herdr pane split --current --direction right
  herdr pane run <pane-id> mr-review follow <dir>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveReviewDir(args[0])
			if err != nil {
				return err
			}
			return runFollow(dir)
		},
	}
}

func runFollow(dir string) error {
	rev, err := store.LoadReview(dir)
	if err != nil {
		return err
	}
	// the transcript to follow: the newest session written after the review
	// started (fall back to review.json's mtime)
	started := time.Now().Add(-24 * time.Hour)
	if t, perr := time.Parse(time.RFC3339, rev.CreatedAt); perr == nil {
		started = t
	} else if fi, serr := os.Stat(filepath.Join(dir, "review.json")); serr == nil {
		started = fi.ModTime()
	}

	rawPath := store.RawFindingsPath(dir)
	showPromptSummary(dir)
	fmt.Printf("following AI review of !%d (%s) — Ctrl-C to detach\n", rev.IID, rev.Project)

	events := make(chan string, 64)
	ctx, cancel := contextWithInterrupt()
	defer cancel()
	go agent.WatchSession(ctx, started, "mr"+strconv.Itoa(rev.IID), func(line string) {
		select {
		case events <- line:
		default: // never block the watcher
		}
	})

	lastFindings := -1
	var shownRaw []string
	idleAfterFindings := 0
	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Println("\ndetached (review continues in the background)")
			return nil
		case line := <-events:
			fmt.Printf("%s %s\n", dimTime(), line)
			idleAfterFindings = 0
		case <-ticker.C:
			// surface findings as they are appended, one by one
			if b, rerr := os.ReadFile(rawPath); rerr == nil {
				rawLines := nonEmptyLines(string(b))
				for i := len(shownRaw); i < len(rawLines); i++ {
					if s := rawFindingSummary(rawLines[i]); s != "" {
						fmt.Printf("%s ⚑ %s\n", dimTime(), s)
					}
					shownRaw = append(shownRaw, rawLines[i])
				}
				if len(rawLines) != lastFindings {
					lastFindings = len(rawLines)
					idleAfterFindings = 0
				}
			}
			// auto-exit once findings exist and the transcript went quiet —
			// the agent is done writing and the CLI flow is wrapping up
			if lastFindings > 0 {
				idleAfterFindings++
				if idleAfterFindings >= 20 { // ~14s of silence
					fmt.Println("\nagent finished writing findings — follow complete.")
					return nil
				}
			}
		}
	}
}

func countLines(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	return strings.Count(string(b), "\n")
}

func dimTime() string {
	if isTTY(os.Stdout) {
		return "\033[2m" + time.Now().Format("15:04:05") + "\033[0m"
	}
	return time.Now().Format("15:04:05")
}

// showPromptSummary prints where the review prompt lives and its opening
// lines, so the operator can see exactly what the agent was asked to do.
func showPromptSummary(dir string) {
	path := filepath.Join(dir, "prompt.txt")
	b, err := os.ReadFile(path)
	if err != nil {
		return // prompt not written yet (older run)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	fmt.Printf("prompt: %s (%d lines)\n", path, len(lines))
	shown := 0
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "MERGE REQUEST") || strings.HasPrefix(l, "MATERIALS") ||
			strings.HasPrefix(l, "WHAT TO") || strings.HasPrefix(l, "SEVERITY") ||
			strings.HasPrefix(l, "KIND") || strings.HasPrefix(l, "OUTPUT CONTRACT") {
			continue
		}
		fmt.Printf("  ┃ %s", truncRunes(l, 100))
		fmt.Println()
		shown++
		if shown >= 2 {
			break
		}
	}
	fmt.Printf("  (full text: mr-review prompt %s · TUI key p)\n\n", dir)
}

// resolveReviewDir accepts either a review dir or a review worktree path
// (~/.local/share/mr-review/worktrees/<slug>-mr<iid>-<sha8>) and returns the
// review dir. On failure it lists the available reviews so the path is easy
// to copy.
func resolveReviewDir(arg string) (string, error) {
	if _, err := store.LoadReview(arg); err == nil {
		return arg, nil
	}
	// MR URL: full coordinates (project + iid).
	if strings.Contains(arg, "/") {
		if ref, perr := gitlab.ParseMRRef(arg, ""); perr == nil {
			cand := store.ReviewDir(ref.Host, ref.Project, ref.IID)
			if _, lerr := store.LoadReview(cand); lerr == nil {
				return cand, nil
			}
			return "", fmt.Errorf("no in-progress review for !%d (%s) — start one: mr-review start %s",
				ref.IID, ref.Project, arg)
		}
	}
	// bare MR number: match an in-progress review across all projects
	if n := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(arg), "!"), "mr"), "MR"); n != "" {
		if iid, ierr := strconv.Atoi(n); ierr == nil {
			matches := scanReviewsByIID(iid)
			switch len(matches) {
			case 1:
				return matches[0], nil
			case 0:
				if ref, perr := gitlab.ParseMRRef(n, ""); perr == nil {
					cand := store.ReviewDir(ref.Host, ref.Project, ref.IID)
					if _, lerr := store.LoadReview(cand); lerr == nil {
						return cand, nil
					}
				}
				return "", fmt.Errorf("no in-progress review for MR !%d — start one: mr-review start !%d", iid, iid)
			default:
				return "", fmt.Errorf("MR !%d exists for %d projects — pass the MR URL or the review dir:\n  %s",
					iid, len(matches), strings.Join(matches, "\n  "))
			}
		}
	}
	// worktree form: <...>-mr<iid>-<sha8>
	base := filepath.Base(strings.TrimRight(arg, "/"))
	if m := reWorktreeDir.FindStringSubmatch(base); m != nil {
		candidate := filepath.Join(store.Home(), m[1], "mr"+m[2])
		if _, err := store.LoadReview(candidate); err == nil {
			return candidate, nil
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%q is not a review dir (needs review.json). Available reviews:", arg)
	matches := allReviewDirs()
	for _, m := range matches {
		fmt.Fprintf(&b, "\n  %s", m)
	}
	if len(matches) == 0 {
		b.WriteString("\n  (none)")
	}
	return "", fmt.Errorf("%s", b.String())
}

var reWorktreeDir = regexp.MustCompile(`^(.+)-mr(\d+)-[0-9a-f]{6,}$`)

// scanReviewsByIID returns the review dirs of every in-progress review of
// MR <iid> across all projects under the state home.
func scanReviewsByIID(iid int) []string {
	var out []string
	projects, _ := os.ReadDir(store.Home())
	for _, p := range projects {
		if !p.IsDir() || p.Name() == "worktrees" || p.Name() == "cache" {
			continue
		}
		cand := filepath.Join(store.Home(), p.Name(), "mr"+strconv.Itoa(iid))
		if _, lerr := store.LoadReview(cand); lerr == nil {
			out = append(out, cand)
		}
	}
	return out
}

// allReviewDirs lists every review dir under the state home.
func allReviewDirs() []string {
	var out []string
	projects, _ := os.ReadDir(store.Home())
	for _, p := range projects {
		if !p.IsDir() || p.Name() == "worktrees" || p.Name() == "cache" {
			continue
		}
		mrs, _ := os.ReadDir(filepath.Join(store.Home(), p.Name()))
		for _, m := range mrs {
			if m.IsDir() && strings.HasPrefix(m.Name(), "mr") {
				out = append(out, filepath.Join(store.Home(), p.Name(), m.Name()))
			}
		}
	}
	return out
}

// rawFindingSummary renders one raw finding line for the live feed.
func rawFindingSummary(line string) string {
	var f struct {
		Severity string `json:"severity"`
		Title    string `json:"title"`
		File     string `json:"file"`
		Line     int    `json:"line"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		return "" // partial write or marker: skip silently
	}
	if f.Title == "" {
		return ""
	}
	loc := f.File
	if f.Line > 0 {
		loc += fmt.Sprintf(":%d", f.Line)
	}
	return f.Severity + " " + f.Title + " (" + loc + ")"
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
