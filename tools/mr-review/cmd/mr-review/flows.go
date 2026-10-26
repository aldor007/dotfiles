package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mr-review/internal/agent"
	"mr-review/internal/gitlab"
	"mr-review/internal/store"
	"mr-review/internal/worktree"

	"github.com/spf13/viper"
)

// short renders a SHA prefix for progress lines.
func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// timeouts resolve via viper: flag > env (MR_REVIEW_TIMEOUT /
// MR_REVIEW_DISCUSS_TIMEOUT) > config file > default.
func reviewTimeout() time.Duration {
	if d := viper.GetDuration("timeout"); d > 0 {
		return d
	}
	return 25 * time.Minute
}

// expectedReviewDuration is the progress-bar denominator: typical full
// review length (config: expected_review_duration). Reviews commonly take
// 5-15 minutes and ~10-15 turns; past 100% the bar shows overtime.
func expectedReviewDuration() time.Duration {
	if d := viper.GetDuration("expected_review_duration"); d > 0 {
		return d
	}
	return 10 * time.Minute
}

func agentRunner() *agent.Runner  { return agent.New() }
func discussCtx() context.Context { return context.Background() }
func discussTimeout() time.Duration {
	if d := viper.GetDuration("discuss_timeout"); d > 0 {
		return d
	}
	return 5 * time.Minute
}

// startReview runs the full flow: fetch MR -> worktree -> diff -> agent ->
// ingest -> TUI (unless noTUI).
func prepareReview(ref *gitlab.MRRef) (string, *store.Review, error) {
	gl := gitlab.New()
	gl.Host = ref.Host
	gl.Log = showCmd

	mr, err := gl.GetMR(ref.Project, ref.IID)
	if err != nil {
		return "", nil, fmt.Errorf("fetching MR: %w", err)
	}
	if mr.DiffRefs.HeadSHA == "" || mr.DiffRefs.BaseSHA == "" {
		return "", nil, fmt.Errorf("MR has no diff refs yet (empty MR?)")
	}

	repoMgr, err := currentRepo()
	if err != nil {
		return "", nil, err
	}
	repoMgr.Log = showCmd

	dir := store.ReviewDir(ref.Host, ref.Project, ref.IID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}

	rev := &store.Review{
		Project:      ref.Project,
		Host:         ref.Host,
		IID:          ref.IID,
		Title:        mr.Title,
		Author:       mr.Author.Username,
		SourceBranch: mr.SourceBranch,
		TargetBranch: mr.TargetBranch,
		HeadSHA:      mr.DiffRefs.HeadSHA,
		BaseSHA:      mr.DiffRefs.BaseSHA,
		StartSHA:     mr.DiffRefs.StartSHA,
		RepoPath:     repoMgr.RepoPath,
		State:        "reviewing",
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if mr.SourceProjectID != 0 && mr.ProjectID != 0 && mr.SourceProjectID != mr.ProjectID {
		sp, err := gl.ProjectPath(mr.SourceProjectID)
		if err == nil {
			rev.SourceProject = sp // fork MR: fetch the ref from the source project
		} else {
			logWarnf("fork source project lookup failed: %v", err)
		}
	}

	st := newSteps()
	statusLine := ""
	if mr.DetailedMergeStatus.Value != "" {
		statusLine = fmt.Sprintf(" [%s]", mr.DetailedMergeStatus.Value)
	}
	st.done("MR !%d fetched: %s%s", mr.IID, trunc(mr.Title, 60), statusLine)

	sp := startSpinner("preparing worktree (git fetch + checkout)")
	wt, err := repoMgr.Ensure(rev)
	if err != nil {
		sp.Stop(false, "worktree preparation failed")
		return "", nil, err
	}
	sp.StopQuiet()
	st.done("worktree ready: %s", relWorktree(wt))

	if err := repoMgr.Diff(wt, rev.BaseSHA, rev.HeadSHA, filepath.Join(dir, "diff.patch")); err != nil {
		return "", nil, fmt.Errorf("diff: %w", err)
	}
	if patch, err := os.ReadFile(filepath.Join(dir, "diff.patch")); err == nil {
		f, a, d := diffStat(patch)
		st.done("diff captured: %d files, +%d/−%d", f, a, d)
	} else {
		st.done("diff captured")
	}

	discs, err := gl.Discussions(ref.Project, ref.IID)
	if err != nil {
		return "", nil, fmt.Errorf("fetching discussions: %w", err)
	}
	if err := saveJSON(filepath.Join(dir, "discussions.json"), discs); err != nil {
		return "", nil, err
	}
	st.done("existing discussions loaded: %d", len(discs))

	rev.Description = mr.Description
	if err := rev.Save(dir); err != nil {
		return "", nil, err
	}
	return dir, rev, nil
}

// startReview prepares the review, spawns the AI review detached (the user
// reviews the diff in the TUI while it runs), and opens the TUI.
func startReview(ref *gitlab.MRRef, noTUI bool) error {
	dir, rev, err := prepareReview(ref)
	if err != nil {
		return err
	}
	if err := store.SpawnReviewBG(dir, rev.HeadSHA); err != nil {
		return fmt.Errorf("spawning background AI review: %w", err)
	}
	if noTUI {
		fmt.Printf("review prepared: %s\nAI review is running in the background — open with: mr-review resume %s\n", dir, dir)
		return nil
	}
	return runTUI(dir)
}

// relWorktree shortens the worktree path under $HOME for display.

// relWorktree shortens the worktree path under $HOME for display.
func relWorktree(wt string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if strings.HasPrefix(wt, home+"/") {
			return "~" + strings.TrimPrefix(wt, home)
		}
	}
	return wt
}

// runAgentReview builds the prompt, runs pi headlessly, ingests findings.
// Every phase reports progress: prompt → agent run (spinner + elapsed) →
// findings validation.
func runAgentReview(gl *gitlab.Client, dir string, rev *store.Review, wt string, discs []gitlab.Discussion, st *steps, description string) error {
	rawDiscs := renderDiscussions(discs)
	in := agent.ReviewInput{
		Project:        rev.Project,
		IID:            rev.IID,
		Title:          rev.Title,
		Author:         rev.Author,
		SourceBranch:   rev.SourceBranch,
		TargetBranch:   rev.TargetBranch,
		HeadSHA:        rev.HeadSHA,
		Worktree:       wt,
		DiffPath:       filepath.Join(dir, "diff.patch"),
		DiscussionsFmt: rawDiscs,
		FindingsOut:    store.RawFindingsPath(dir),
		Description:    description,
		JiraKeys:       agent.ExtractJiraKeys(rev.Title, description),
		ContextFiles:   store.ContextFiles(rev.Host, rev.Project),
	}
	// previous findings: the agent verifies each instead of re-reporting
	// duplicates (the merge in agent.Refresh/Ingest matches by file+issue)
	if prev, perr := store.LoadFindings(dir); perr == nil && len(prev) > 0 {
		var b strings.Builder
		for _, f := range prev {
			state := f.Decision
			if f.Publication != nil {
				state = "published"
			} else if f.Stale {
				state += " (stale)"
			}
			fmt.Fprintf(&b, "- %s %s:%d [%s %s] %s\n", f.ID, f.File, f.Line, f.Severity, state, f.Title)
		}
		in.PreviousFindings = b.String()
		st.done("previous review pass found %d findings — the agent will verify them", len(prev))
	}

	// kimchi-native parallelism: for larger MRs, pre-compute balanced file
	// groups and let the agent spawn its own reviewer subagents.
	if changed, cerr := gl.ChangedFiles(rev.Project, rev.IID); cerr == nil &&
		len(changed) >= parallelThreshold() {
		groups := agent.ShardFiles(changed, maxSubagents())
		in.FileGroups = groups
		in.MaxSubagents = len(groups)
		st.done("parallel review: %d files → %d subagent groups", len(changed), len(groups))
	}
	prompt := agent.BuildPrompt(in)
	if err := os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte(prompt), 0o600); err != nil {
		return err
	}
	st.done("review prompt built (%d lines)", strings.Count(prompt, "\n"))

	_ = os.Remove(store.RawFindingsPath(dir)) // stale output from a prior run
	started := time.Now()
	sp := startSpinnerExpected(fmt.Sprintf("AI review running (%s -p, timeout %s)", agent.New().Bin, reviewTimeout()),
		expectedReviewDuration())
	onLine := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" || agent.IsNoise(line) {
			return
		}
		// single-line live activity (no scroll spam): what the agent is doing
		// right now — tool calls, commands it runs, reading/writing files
		sp.Update(line)
		// full stream at debug level
		logDebugf("agent: %s", truncRunes(line, 110))
	}
	// kimchi -p is silent on stdout until it finishes: live "what is it
	// doing" comes from its session transcript instead.
	watchCtx, cancelWatch := context.WithCancel(context.Background())
	defer cancelWatch()
	go agent.WatchSession(watchCtx, started, fmt.Sprintf("mr%d", rev.IID), func(line string) {
		sp.Update(line)
		logDebugf("agent: %s", truncRunes(line, 110))
	})
	// live findings counter: the agent appends as it verifies, surface the
	// count on the spinner instead of waiting for the whole run
	go func() {
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			if b, err := os.ReadFile(store.RawFindingsPath(dir)); err == nil {
				n := 0
				for _, l := range strings.Split(string(b), "\n") {
					if strings.TrimSpace(l) != "" {
						n++
					}
				}
				sp.SetCount(n)
			}
		}
	}()
	rerr := agent.New().RunReview(context.Background(), wt, prompt,
		filepath.Join(dir, "agent.log"), reviewTimeout(), onLine)
	if rerr != nil {
		sp.Stop(false, fmt.Sprintf("AI review failed after %s (log: %s/agent.log)",
			time.Since(started).Round(time.Second), relWorktree(dir)))
		return rerr
	}
	sp.Stop(true, fmt.Sprintf("AI review finished in %s", time.Since(started).Round(time.Second)))

	res, err := agent.Ingest(dir)
	if err != nil {
		return err
	}
	summary := fmt.Sprintf("findings validated: %d added, %d dropped", res.Added, len(res.Dropped))
	if len(res.Dropped) > 0 {
		summary += " — " + strings.Join(res.Dropped, "; ")
	}
	st.done("%s", summary)
	return nil
}

// refreshReview re-reviews after the MR moved, preserving decisions.
func refreshReview(dir string) error {
	rev, err := store.LoadReview(dir)
	if err != nil {
		return err
	}
	gl := gitlab.New()
	gl.Host = rev.Host
	gl.Log = showCmd
	mr, err := gl.GetMR(rev.Project, rev.IID)
	if err != nil {
		return fmt.Errorf("fetching MR: %w", err)
	}
	if mr.DiffRefs.HeadSHA == rev.HeadSHA {
		fmt.Println("MR unchanged; nothing to refresh.")
		return nil
	}
	rev0SHA := rev.HeadSHA
	oldFindings, err := store.LoadFindings(dir)
	if err != nil {
		return err
	}
	rev.HeadSHA = mr.DiffRefs.HeadSHA
	rev.BaseSHA = mr.DiffRefs.BaseSHA
	rev.StartSHA = mr.DiffRefs.StartSHA
	rev.Title = mr.Title

	repoMgr, err := worktree.New(rev.RepoPath)
	if err != nil {
		return err
	}
	st := newSteps()
	st.done("MR updated: reviewed head %s → current %s", short(rev0SHA), short(rev.HeadSHA))

	sp := startSpinner("preparing updated worktree (git fetch + checkout)")
	wt, err := repoMgr.Ensure(rev)
	if err != nil {
		sp.Stop(false, "worktree update failed")
		return err
	}
	sp.StopQuiet()
	st.done("worktree updated: %s", relWorktree(wt))
	if err := repoMgr.Diff(wt, rev.BaseSHA, rev.HeadSHA, filepath.Join(dir, "diff.patch")); err != nil {
		return err
	}
	discs, err := gl.Discussions(rev.Project, rev.IID)
	if err != nil {
		return err
	}
	st.done("diff + discussions refreshed")
	if err := runAgentReview(gl, dir, rev, wt, discs, st, mr.Description); err != nil {
		return err
	}

	// merge: validated fresh findings + carried-over human state
	fresh, _, err := agent.ParseRaw(dir)
	if err != nil {
		return err
	}
	merged := agent.Renumber(agent.Refresh(oldFindings, fresh))
	if err := store.SaveFindings(dir, merged); err != nil {
		return err
	}
	if err := rev.Save(dir); err != nil {
		return err
	}
	stale := 0
	for _, f := range merged {
		if f.Stale {
			stale++
		}
	}
	fmt.Printf("refresh complete: %d findings (%d stale, decisions preserved)\n", len(merged), stale)
	return nil
}

func finishAndOpen(dir string, noTUI bool) error {
	if noTUI || !isTTY(os.Stdin) {
		fmt.Printf("review ready: %s\n(next: mr-review tui %s)\n", dir, dir)
		return nil
	}
	return runTUI(dir)
}

func saveJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func renderDiscussions(discs []gitlab.Discussion) string {
	var lines []string
	for _, d := range discs {
		for _, n := range d.Notes {
			if n.System {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s: %s", n.Author.Username,
				replaceNewlines(n.Body)))
		}
	}
	if len(lines) == 0 {
		return "none"
	}
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

func replaceNewlines(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, ' ')
		} else {
			out = append(out, s[i])
		}
	}
	return string(out)
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func parallelThreshold() int {
	if s := os.Getenv("MR_REVIEW_PARALLEL_THRESHOLD"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 15
}

func maxSubagents() int {
	if s := os.Getenv("MR_REVIEW_MAX_SUBAGENTS"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 4
}
