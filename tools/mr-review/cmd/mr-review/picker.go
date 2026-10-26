package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"mr-review/internal/gitlab"
	"mr-review/internal/store"
	"mr-review/internal/tui"

	"github.com/charmbracelet/huh"
)

// runPicker is the no-arg entry. Interactive UX: huh selects with
// type-to-filter — choose filter criteria, pick an MR, or paste an MR
// URL/number. Non-interactive (piped) falls back to plain numbered prompts
// with the "review" default.
//
// Popup-safety: herdr popups close the instant the command exits, so every
// fatal condition prints its message AND waits for Enter.
func runPicker(labels []string) error {
	gl := gitlab.New()
	cwd, _ := os.Getwd()
	project, _, projErr := remoteProjectOf(cwd)

	// not in a GitLab repo: still allow pasting an MR URL
	if projErr != nil {
		fmt.Printf("GitLab MR review\n\ncurrent directory is not a GitLab repo (%s).\n", cwd)
		fmt.Print("Paste an MR URL (https://host/grp/proj/-/merge_requests/N), or press Enter to close: ")
		line, rerr := bufio.NewReader(os.Stdin).ReadString('\n')
		line = strings.TrimSpace(line)
		if rerr != nil || line == "" {
			return waitExit("closed")
		}
		return startFromInput(line)
	}

	me, err := gl.CurrentUser()
	if err != nil {
		fmt.Printf("GitLab MR review — %s\n\nglab auth problem: %v\n", project, err)
		return waitExit("run 'glab auth status' in a pane, then try again")
	}
	interactive := isTTY(os.Stdin)

	// ---- filter criteria ----------------------------------------------------
	criterion := "review"
	if len(labels) > 0 {
		criterion = "label"
	} else if interactive {
		var choice string
		sel := huh.NewSelect[string]().
			Title(fmt.Sprintf("GitLab MR review — %s (you: %s)", project, me)).
			Options(
				huh.NewOption("waiting for my review", "review"),
				huh.NewOption("assigned to me", "assigned"),
				huh.NewOption("authored by me", "author"),
				huh.NewOption("by label", "label"),
				huh.NewOption("all open MRs", "all"),
				huh.NewOption("paste MR URL / number", "url"),
			).Value(&choice).
			Filtering(true).
			Height(8)
		if err := huh.NewForm(huh.NewGroup(sel)).Run(); err != nil {
			return waitExit("cancelled") // Esc / Ctrl-C
		}
		switch choice {
		case "url":
			return promptAndStartMR()
		case "label":
			var lblInput string
			inp := huh.NewInput().
				Title("Labels (comma-separated, ALL must match)").
				Placeholder("code-review,go").
				Value(&lblInput)
			if err := huh.NewForm(huh.NewGroup(inp)).Run(); err != nil {
				return waitExit("cancelled")
			}
			labels = splitLabels(lblInput)
			if len(labels) == 0 {
				return pickError("no labels given")
			}
			criterion = "label"
		default:
			criterion = choice
		}
	}

	// ---- fetch the chosen section (single cached, timeout-guarded call) -----
	var items []gitlab.MRListItem
	fetchErr := func() error {
		switch criterion {
		case "review":
			items, err = gl.ListReviewRequested(project, me, 2)
		case "assigned":
			items, err = gl.ListAssigned(project, me, 2)
		case "author":
			items, err = gl.ListAuthored(project, me, 2)
		case "label":
			items, err = gl.ListLabeled(project, labels, 2)
		default:
			items, err = gl.ListOpenMRs(project, 2)
		}
		return err
	}()
	if fetchErr != nil {
		return pickError("listing MRs failed: %v", fetchErr)
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].IID < items[j].IID })
	if len(items) == 0 {
		fmt.Printf("GitLab MR review — %s (you: %s)\n\nno open MRs for this filter (%s).\n",
			project, me, criterion)
		fmt.Print("Paste an MR URL or MR number, or press Enter to close: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return waitExit("closed")
		}
		return startFromInput(line)
	}

	// ---- MR selection -------------------------------------------------------
	if interactive {
		rows := make([]tui.MRRow, 0, len(items))
		for _, it := range items {
			rows = append(rows, tui.MRRow{
				IID: it.IID, Title: it.Title, Author: it.Author.Username,
				SourceBranch: it.SourceBranch, TargetBranch: it.TargetBranch,
				Labels: it.Labels, UpdatedAt: it.UpdatedAt,
			})
		}
		iid, err := tui.RunMRSelect(fmt.Sprintf("MR to review — %s", project), rows)
		if err != nil {
			return pickError("selector failed: %v", err)
		}
		if iid == 0 {
			return waitExit("closed")
		}
		return startReview(&gitlab.MRRef{Project: project, IID: iid}, false)
	}

	// non-interactive fallback: numbered list
	fmt.Printf("GitLab MR review — %s (you: %s, filter: %s)\n\n", project, me, criterion)
	for i, it := range items {
		lbl := ""
		if len(it.Labels) > 0 {
			lbl = " {" + strings.Join(it.Labels, ",") + "}"
		}
		fmt.Printf("  %2d. !%d  %s  (%s, %s→%s)%s\n", i+1, it.IID,
			trunc(it.Title, 60), it.Author.Username, it.SourceBranch, it.TargetBranch, lbl)
	}
	fmt.Print("\nnumber, MR URL, or MR number: ")
	rd := bufio.NewReader(os.Stdin)
	line, _ := rd.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return nil // user chose to close
	}
	if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(items) {
		return startReview(&gitlab.MRRef{Project: project, IID: items[n-1].IID}, false)
	}
	return startFromInput(line)
}

// promptAndStartMR asks for an MR URL/number via huh and starts the review.
func promptAndStartMR() error {
	var input string
	inp := huh.NewInput().
		Title("MR URL or number").
		Placeholder("https://gitlab.com/grp/proj/-/merge_requests/123").
		Value(&input)
	if err := huh.NewForm(huh.NewGroup(inp)).Run(); err != nil {
		return waitExit("cancelled")
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return waitExit("closed")
	}
	return startFromInput(input)
}

// isMRLine reports whether input looks like an MR URL or numeric MR id
// (as opposed to a label name).
func isMRLine(line string) bool {
	if strings.Contains(line, "/-/merge_requests/") {
		return true
	}
	_, err := strconv.Atoi(line)
	return err == nil
}

// startFromInput resolves a pasted MR URL / numeric IID and starts (or
// resumes) the review. Errors are shown with an Enter-wait so popup users
// can actually read them.
func startFromInput(line string) error {
	ref, dir, err := resolveRef(line)
	if err != nil {
		return pickError("cannot parse %q as MR URL or number: %v", line, err)
	}
	if dir != "" {
		return runTUI(dir)
	}
	if err := startReview(ref, false); err != nil {
		return pickError("review failed: %v", err)
	}
	return nil
}

// pickError prints the reason and (on a TTY) waits for Enter — herdr popups
// close the instant the command exits, which would otherwise hide the error.
func pickError(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	if !isTTY(os.Stdin) {
		return err
	}
	fmt.Printf("\nmr-review: error: %v\n", err)
	return waitExit("press Enter to close")
}

// waitExit prints a hint and blocks until Enter (TTY only).
func waitExit(hint string) error {
	if isTTY(os.Stdin) {
		fmt.Printf("%s — press Enter to close: ", hint)
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	return nil
}

func sectionPrio(label string) int {
	switch {
	case strings.HasPrefix(label, "[review]"):
		return 0
	case strings.HasPrefix(label, "[assigned]"):
		return 1
	case strings.HasPrefix(label, "[author]"):
		return 2
	default:
		return 3
	}
}

func remoteProjectOf(cwd string) (string, string, error) {
	out, err := osExecGit(cwd, "remote", "get-url", "origin")
	if err != nil {
		return "", "", err
	}
	u := strings.TrimSpace(out)
	u = strings.TrimSuffix(u, ".git")
	switch {
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "http://"):
		rest := u[strings.Index(u, "://")+3:]
		return rest[strings.Index(rest, "/")+1:], rest[:strings.Index(rest, "/")], nil
	case strings.HasPrefix(u, "git@"):
		rest := strings.TrimPrefix(u, "git@")
		i := strings.Index(rest, ":")
		if i < 0 {
			return "", "", fmt.Errorf("bad ssh remote")
		}
		return rest[i+1:], rest[:i], nil
	}
	return "", "", fmt.Errorf("unrecognized remote %q", u)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func hostOrInferred(host string) string {
	if host == "" {
		return "inferred by glab"
	}
	return host
}

// listMRs prints MRs worth reviewing (label-filtered when labels are set).
func listMRs(labels []string) error {
	gl := gitlab.New()
	cwd, _ := os.Getwd()
	project, host, err := remoteProjectOf(cwd)
	if err != nil {
		return err
	}
	me, err := gl.CurrentUser()
	if err != nil {
		return fmt.Errorf("glab auth: %w", err)
	}
	type section struct {
		name string
		f    func() ([]gitlab.MRListItem, error)
	}
	var sections []section
	if len(labels) > 0 {
		sections = append(sections, section{
			"labeled " + strings.Join(labels, ","),
			func() ([]gitlab.MRListItem, error) { return gl.ListLabeled(project, labels, 2) }})
	} else {
		sections = append(sections,
			section{"waiting for my review", func() ([]gitlab.MRListItem, error) { return gl.ListReviewRequested(project, me, 2) }},
			section{"assigned to me", func() ([]gitlab.MRListItem, error) { return gl.ListAssigned(project, me, 2) }},
			section{"authored by me", func() ([]gitlab.MRListItem, error) { return gl.ListAuthored(project, me, 2) }},
			section{"other open MRs", func() ([]gitlab.MRListItem, error) { return gl.ListOpenMRs(project, 2) }})
	}
	seen := map[int]bool{}
	fmt.Printf("open MRs in %s (gitlab host: %s, you: %s)\n", project, hostOrInferred(host), me)
	for _, s := range sections {
		items, err := s.f()
		if err != nil {
			logWarnf("%s: %v", s.name, err)
			continue
		}
		fmt.Printf("\n%s:\n", s.name)
		shown := 0
		for _, it := range items {
			if seen[it.IID] {
				continue
			}
			seen[it.IID] = true
			exists := ""
			if _, err := os.Stat(store.ReviewDir(hostOrInferred(host), project, it.IID)); err == nil {
				exists = "  [review exists]"
			}
			lbl := ""
			if len(it.Labels) > 0 {
				lbl = "  \uf412{" + strings.Join(it.Labels, ",") + "}"
			}
			age := ""
			if !it.UpdatedAt.IsZero() {
				age = fmt.Sprintf("  \uf43a%s", tui.RelAge(it.UpdatedAt, time.Now()))
			}
			fmt.Printf("  \uf409 !%-5d \033[1m%s\033[0m  \uf508%s \uf418%s→%s%s%s\n", it.IID,
				trunc(it.Title, 64), it.Author.Username, it.SourceBranch, it.TargetBranch, lbl, exists+age)
			shown++
		}
		if shown == 0 {
			fmt.Println("  (none)")
		}
	}
	return nil
}

func osExecGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
