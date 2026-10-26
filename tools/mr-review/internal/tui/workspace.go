// workspace.go — mr-review-owned split workspace: the AI triage cockpit on
// the left, a real tuicr running in a PTY on the right (internal/plex).
// One process, one TTY, no herdr, no tmux. Mouse works in the tuicr pane
// (SGR forwarding with pane-relative coordinates); Ctrl+G / Ctrl+O / Tab
// and click move focus; the footer documents the focused pane's keys.
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mr-review/internal/plex"
	"mr-review/internal/tuicr"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
)

const (
	wsFocusCockpit = iota
	wsFocusTuicr
)

// workspace state on the model.
//
// wsPane != nil ⇔ the split workspace is open.
//
// Lifecycle ownership (review P1-1): the pane is owned by the model —
// openTuicrPane/closeTuicrPane are the ONLY production mutation points,
// and closeTuicrPane is reached from every exit path (W, Ctrl+C, Esc-quit,
// wsPaneExitMsg, plus the guaranteed defer around p.Run() in Run and
// RunWorkspace). The variables remain package-level purely as an accessor
// shim: registry.go, nav.go and view.go (and legacy tests) read wsPane
// directly and are outside this fix's ownership scope — migrating those
// readers to model fields is the follow-up that deletes this shim.
var (
	wsPane       *plex.Pane
	wsPaneCancel context.CancelFunc // cancellable context for the pane lifecycle (P1 #9)
	wsFocus      int
	wsExited     bool
	wsErr        error
	// wsDetail shows the selected finding's details inside the cockpit
	// strip (instead of the list) — opening details must not take over
	// the whole screen.
	wsDetail    bool
	wsDetailTop int
	wsAutoOpen  bool
)

// openTuicrPane spawns tuicr in a PTY pane (registry action W).
//
// REVIEW FIXES (P1 #9 + P2 #10):
//   - PTY lifecycle: the embedded pane is now model-owned. A cancellable
//     context (wsPaneCancel) is created alongside the pane and is always
//     invoked from closeTuicrPane — which is now reached from every
//     exit path (tea.Quit, wsPaneExitMsg, model teardown). The previous
//     implementation used context.Background() for the pane and never
//     cancelled it on non-error exits, so the tuicr subprocess could
//     outlive the host UI.
//   - Async PrepareSession: the synchronous 30s-timeout PrepareSession
//     call blocked the TUI (P2 #10). Now we launch the pane immediately
//     and run PrepareSession in the background as a tea.Cmd; the model
//     receives a wsPrepareDoneMsg and updates m.msg accordingly. Manual
//     review can start immediately, status messages catch up.
func (m *model) openTuicrPane() tea.Cmd {
	if wsPane != nil {
		wsFocus = wsFocusTuicr
		return nil // already open
	}
	if m.rev == nil || m.rev.Worktree == "" {
		m.msg = "no worktree — prepare the review first"
		return nil
	}

	w, h := m.wsPaneSize()
	args := m.tuicrPaneArgs()
	// model-owned cancellable context for the pane: closed by
	// closeTuicrPane on every exit path (tea.Quit, wsPaneExitMsg).
	paneCtx, paneCancel := context.WithCancel(context.Background())
	p, err := plex.Start(paneCtx, m.rev.Worktree, w, h, nil, tuicrBin(), args...)
	if err != nil {
		paneCancel()
		m.msg = "tuicr pane failed: " + err.Error()
		return nil
	}
	wsPane = p
	wsPaneCancel = paneCancel
	wsExited = false
	wsErr = nil
	wsFocus = wsFocusTuicr
	m.msg = "tuicr opened — Ctrl+O/Ctrl+G toggles focus · W closes"

	// Kick off the async session check (P2 #10); the model updates the
	// status message when wsPrepareDoneMsg arrives.
	expected := m.expectedDiffFiles()
	if expected == 0 {
		return tea.Batch(waitPaneExit(p), wsBootTick())
	}
	return tea.Batch(waitPaneExit(p), wsBootTick(), wsPrepareSessionCmd(m.tuicrClient(),
		m.rev.Worktree, m.rev.Project, m.rev.IID, expected))
}

// closeTuicrPane kills the embedded tuicr (W in workspace mode) and
// releases the model-owned cancellable context. Safe to call multiple
// times (REVIEW FIX P1 #9). Reached from every exit path so the tuicr
// subprocess cannot outlive the host UI: the model quit paths (Ctrl+C,
// Esc-quit), W-close, and the guaranteed defer around p.Run() in Run and
// RunWorkspace (covers the registry's q action and any abnormal exit).
func (m *model) closeTuicrPane() {
	if wsPane == nil {
		return
	}
	if wsPaneCancel != nil {
		wsPaneCancel() // unblock any context-aware cleanup in plex
		wsPaneCancel = nil
	}
	wsPane.Kill()
	wsPane = nil
	wsFocus = wsFocusCockpit
	if m.msg == "" {
		m.msg = "tuicr pane closed"
	}
}

func waitPaneExit(p *plex.Pane) tea.Cmd {
	return func() tea.Msg {
		<-p.Exited()
		return wsPaneExitMsg{}
	}
}

// wsPrepareSessionCmd runs PrepareSession in the background so manual
// review can start without waiting on the 30s-per-call tuicr CLI. The
// status it reports becomes a model message via wsPrepareDoneMsg.
func wsPrepareSessionCmd(cli *tuicr.CLI, worktree, project string, iid, expected int) tea.Cmd {
	return func() tea.Msg {
		slug, status, err := cli.PrepareSession(context.Background(), worktree, project, iid, expected)
		return wsPrepareDoneMsg{slug: slug, status: status, err: err}
	}
}

// wsPrepareDoneMsg is delivered when the async PrepareSession resolves.
// The Update loop merges it into the model msg WITHOUT blocking the TUI.
type wsPrepareDoneMsg struct {
	slug   string
	status tuicr.SessionStatus
	err    error
}

// wsPaneExitMsg fires when the embedded tuicr process ends.
type wsPaneExitMsg struct{}

// wsBootTickMsg drives the boot loader spinner while tuicr starts up.
type wsBootTickMsg struct{}

func wsBootTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return wsBootTickMsg{} })
}

// wsDataMsg fires when the embedded pane's screen changed — the workspace
// re-renders immediately (event-driven, no polling lag).
type wsDataMsg struct{}

// waitPaneData blocks until the pane's screen changes or the child exits.
func waitPaneData(p *plex.Pane) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-p.Changed():
		case <-p.Exited():
		}
		return wsDataMsg{}
	}
}

// wsDataPump re-arms the pane listener while the pane is alive.
func (m *model) wsDataPump() tea.Cmd {
	if wsPane != nil && wsPane.Alive() {
		return waitPaneData(wsPane)
	}
	return nil
}

// bgLastActivity reads the agent's most recent output line from agent.log —
// the "what is it doing right now" progress hint.
func bgLastActivity(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "agent.log"))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(stripANSIish(lines[i]))
		if l != "" {
			return truncPlain(l, 110)
		}
	}
	return ""
}

// bgElapsed renders the compact elapsed time since the bg job started.
func (m *model) bgElapsed() string {
	if m.bgReview == nil || m.bgReview.StartedAt == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, m.bgReview.StartedAt)
	if err != nil {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// wsPaneSize computes the tuicr pane geometry: the inner area of the right
// box (which occupies everything left of the cockpit column).
func (m *model) wsPaneSize() (int, int) {
	w := m.width - m.wsCockpitWidth() - 1 - 2 // gap + box borders
	h := m.bodyHeight() - 2
	if w < 20 {
		w = 20
	}
	if h < 6 {
		h = 6
	}
	return w, h
}

// wsCockpitWidth is the left (cockpit) column width. While a finding's
// detail peek is open the cockpit EXPANDS (prose needs room to be readable)
// and the tuicr pane shrinks — its PTY is resized so tuicr reflows; on Esc
// everything restores. Capped so tuicr never disappears entirely.
func (m *model) wsCockpitWidth() int {
	if wsDetail {
		w := m.width * 64 / 100
		if w < 84 {
			w = 84
		}
		if w > 120 {
			w = 120
		}
		if w > m.width-40 {
			w = max(60, m.width-40)
		}
		return w
	}
	w := m.width * 40 / 100
	if w < 46 {
		w = 46
	}
	if w > 64 {
		w = 64
	}
	if w > m.width-44 {
		w = max(30, m.width-44)
	}
	return w
}

// mrURL builds the tuicr PR target URL (empty when unknown).
func (m *model) mrURL() string {
	if m.rev == nil || m.rev.Host == "" || m.rev.Project == "" {
		return ""
	}
	h := strings.TrimPrefix(strings.TrimPrefix(m.rev.Host, "https://"), "http://")
	return fmt.Sprintf("https://%s/%s/-/merge_requests/%d", strings.TrimSuffix(h, "/"), m.rev.Project, m.rev.IID)
}

// tuicrPaneArgs builds the tuicr launch arguments: PR mode (tuicr's native
// GitLab integration — :submit works, the MR diff comes from GitLab).
//
// KNOWN UPSTREAM LIMITATION (verified against tuicr 0.27.0): tuicr's revset
// handling mangles explicit SHA ranges — "-r <sha>..<sha>" errors in PR
// mode ("Malformed Gerrit change target") and produces partial/inverted
// diffs in local mode; single-SHA revsets review only that one commit.
// Files tuicr's fetch drops (observed: charts/ templates on this MR) remain
// fully visible in the cockpit's continuous diff (view 2) and in each
// finding's detail diff section — and mr-review-publish posts them inline
// on GitLab regardless.
func (m *model) tuicrPaneArgs() []string {
	if mrURL := m.mrURL(); mrURL != "" {
		return []string{"pr", mrURL}
	}
	return []string{"-r", m.rev.SourceBranch}
}

// expectedDiffFiles counts the files in the review's diff.patch — the
// number of files a complete tuicr session must contain.
func (m *model) expectedDiffFiles() int {
	b, err := os.ReadFile(filepath.Join(m.dir, "diff.patch"))
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "diff --git ") {
			n++
		}
	}
	return n
}

func tuicrBin() string {
	if b := os.Getenv("MR_REVIEW_TUICR"); b != "" {
		return b
	}
	return "tuicr"
}

// ---- layout ----------------------------------------------------------------

// wsView renders the split workspace: two bordered panes with titles; the
// focused pane's border is bright, the other dim. Every row is assembled to
// EXACTLY m.width cells (x/ansi-aware measuring) — a wider row would wrap
// in the terminal and stack the panes on top of each other.
func (m *model) wsView(w, h int) string {
	cw := m.wsCockpitWidth()
	tw := w - cw - 1 // 1-cell gap
	if tw < 24 {
		tw = 24
	}
	inner := h - 2

	var left, right []string
	left = m.wsCockpitLines(cw-2, inner, true)
	if wsPane != nil && !wsExited {
		right = ansiLines(wsPane.Render())
		if wsBooting(right) {
			right = m.wsBootingView(tw-2, inner)
		}
	} else {
		right = wsPlaceholder(tw-2, inner)
	}

	// Nerd Font icons (JetBrains Mono NF, same convention as mrselect)
	const (
		glyphCockpit = "\uf085" // cogs — AI triage
		glyphTuicr   = "\uf409" // git PR — manual review
	)
	cockpitTitle := fmt.Sprintf("\uf085 AI cockpit · !%d", m.rev.IID)
	tuicrTitle := "\uf409 tuicr — manual review"
	if wsFocus == wsFocusCockpit {
		cockpitTitle = "\uf111 " + cockpitTitle // ● filled circle
		tuicrTitle = "\uf10c " + tuicrTitle     // ○ open circle
	} else {
		cockpitTitle = "\uf10c " + cockpitTitle
		tuicrTitle = "\uf111 " + tuicrTitle
	}

	rows := make([]string, 0, h)
	rows = append(rows,
		wsFit(wsBoxTop(cw, cockpitTitle, wsFocus == wsFocusCockpit), w-cw-1)+" "+
			wsFit(wsBoxTop(tw, tuicrTitle, wsFocus == wsFocusTuicr), tw))
	for i := 0; i < inner; i++ {
		l := wsBoxRow(pad(cut(lineAt(left, i), cw-2), cw-2), wsFocus == wsFocusCockpit)
		r := wsBoxRow(pad(cut(lineAt(right, i), tw-2), tw-2), wsFocus == wsFocusTuicr)
		rows = append(rows, wsFit(l, w-cw-1)+" "+wsFit(r, tw))
	}
	rows = append(rows,
		wsFit(wsBoxBottom(cw, wsFocus == wsFocusCockpit), w-cw-1)+" "+
			wsFit(wsBoxBottom(tw, wsFocus == wsFocusTuicr), tw))
	return strings.Join(rows, "\n")
}

// wsFit clamps a box segment to exactly w cells (ANSI-aware) so a composed
// row can never exceed the terminal width and wrap.
func wsFit(s string, w int) string {
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "")
}

// wsBoxTop draws a top border with an embedded title.
func wsBoxTop(w int, title string, focused bool) string {
	col := styleBorder
	if focused {
		col = styleBorderF
	}
	t := truncPlain(title, max(6, w-4))
	tw := ansi.StringWidth(t)
	plain := "╭" + t + strings.Repeat("─", max(0, w-2-tw)) + "╮"
	return col.Render(plain)
}

func wsBoxRow(content string, focused bool) string {
	col := styleBorder
	if focused {
		col = styleBorderF
	}
	return col.Render("│") + content + col.Render("│")
}

func wsBoxBottom(w int, focused bool) string {
	col := styleBorder
	if focused {
		col = styleBorderF
	}
	return col.Render("╰" + strings.Repeat("─", max(0, w-2)) + "╯")
}

// wsCockpitView renders the compact cockpit strip for the workspace's left
// column (kept for tests/compat).
func (m *model) wsCockpitView(w, h int) []string {
	return m.wsCockpitLines(w, h, true)
}

func (m *model) wsCockpitViewDim(w, h int) []string {
	return m.wsCockpitLines(w, h, false)
}

// wsCockpitLines renders the cockpit strip: header, view tabs, AI status,
// then the active view's content — or the selected finding's details when
// the detail peek is open (Esc returns to the list).
func (m *model) wsCockpitLines(w, h int, focused bool) []string {
	var out []string
	hdr := fmt.Sprintf("AI COCKPIT !%d", m.rev.IID)
	if focused {
		out = append(out, styleBorderF.Render(cut("● "+hdr, w)))
	} else {
		out = append(out, styleDim.Render(cut("○ "+hdr, w)))
	}
	out = append(out, m.wsViewTabs(w))
	if m.bgReview != nil {
		out = append(out, cut(truncPlain(m.bgStatusLine(), w), w))
	}
	out = append(out, styleBorder.Render(strings.Repeat("─", max(4, w-2))))

	if wsDetail && m.current() != nil {
		out = append(out, m.wsDetailView(w, h-len(out))...)
		return out
	}

	switch m.leftView {
	case viewFiles:
		out = append(out, m.filesView()...)
	case viewComments:
		out = append(out, m.commentsView()...)
	default:
		out = append(out, m.findingsView()...)
	}
	return out
}

// wsDetailView renders the selected finding inside the cockpit strip —
// details without taking over the screen (the tuicr pane keeps running).
func (m *model) wsDetailView(w, h int) []string {
	f := m.current()
	if f == nil {
		return []string{styleDim.Render("no finding selected")}
	}
	var out []string
	hdr := fmt.Sprintf("%s %s — %s", sevChip(f), f.ID, decisionLabel(f))
	out = append(out, styleHeader.Render(cut(hdr, w)))
	out = append(out, cut(styleSel.Render(truncPlain(f.Title, w)), w))
	loc := fmt.Sprintf("%s:%d", f.File, f.Line)
	if f.LineEnd > f.Line {
		loc += fmt.Sprintf("-%d", f.LineEnd)
	}
	out = append(out, styleDim.Render(cut(loc+" · "+f.Kind+" · "+f.Confidence, w)))
	out = append(out, styleBorder.Render(strings.Repeat("─", max(4, w-2))))

	// the code diff around the finding (focused window, highlighted)
	m.ensureDiff()
	if fd := m.diffParsed[f.File]; fd != nil {
		out = append(out, styleSection.Render(cut("diff — "+shortPath(f.File), w)))
		rows, hl := DiffWindow(fd, f.Side, f.Line, f.LineEnd, 6)
		texts := make([]string, len(rows))
		for i, r := range rows {
			texts[i] = r.Text
		}
		hlTexts := HighlightedCodeLines(f.File, texts, plainMode())
		for _, l := range RenderDiffLines(rows, f.Side, hl, w, plainMode(), hlTexts) {
			out = append(out, l)
		}
		out = append(out, "")
	} else {
		out = append(out, styleDim.Render(cut("file not part of this diff — open tuicr for the full change", w)))
	}

	out = append(out, styleSection.Render(cut("explanation", w)))
	out = append(out, "")
	for _, l := range wrap(f.Explanation, w) {
		out = append(out, cut(l, w))
	}
	if strings.TrimSpace(f.Evidence) != "" {
		out = append(out, "")
		out = append(out, styleSection.Render(cut("evidence", w)))
		for _, l := range wrap(f.Evidence, w) {
			out = append(out, cut(l, w))
		}
	}
	if c := f.EffectiveComment(); strings.TrimSpace(c) != "" {
		out = append(out, "")
		out = append(out, styleSection.Render(cut("suggested comment", w)))
		for _, l := range wrap(c, w) {
			out = append(out, cut(l, w))
		}
	}
	// scroll: keep the hint line visible, slice the body from wsDetailTop
	hint := styleDim.Render(cut("Esc back · a approve · r reject · S stage→tuicr · j/k scroll", w))
	body := out
	if wsDetailTop > 0 && wsDetailTop < len(body) {
		body = body[wsDetailTop:]
	} else if wsDetailTop >= len(body) {
		wsDetailTop = max(0, len(body)-1)
		body = out[wsDetailTop:]
	}
	out = append(body, hint)
	return out
}

// wsDetailScroll scrolls the detail peek (j/k while it is open).
func (m *model) wsDetailScroll(dir int) {
	wsDetailTop += dir
	if wsDetailTop < 0 {
		wsDetailTop = 0
	}
}

// Nerd Font glyphs for the cockpit view tabs.
const (
	glyphTabFindings = "\uf188" // bug
	glyphTabFiles    = "\uf07b" // folder
	glyphTabComments = "\uf0e5" // comment-o
	glyphTabInFile   = "\uf016" // file-code-o
)

// wsViewTabs renders the cockpit view tabs (FINDINGS · FILES · COMMENTS)
// with Nerd Font icons and the active view highlighted. When the
// file-findings submenu is active, FILES expands into a breadcrumb:
// FILES ▸ <file> (Esc pops back to FILES).
func (m *model) wsViewTabs(w int) string {
	activeStyle := lipgloss.NewStyle().Bold(true).
		Background(lipgloss.Color("39")).Foreground(lipgloss.Color("235"))
	dimTab := func(icon, label string) string {
		return styleDim.Render(" " + icon + " " + label + " ")
	}
	act := func(icon, label string) string {
		return activeStyle.Render(" " + icon + " " + label + " ")
	}
	sep := styleBorder.Render("│")

	if m.leftView == viewFileFindings {
		// submenu breadcrumb: FILES ▸ <file>
		parts := []string{dimTab("\uf07b", "FILES"), sep,
			act("\uf016", shortPath(m.fileFindingsFile))}
		return cut(strings.Join(parts, sep), w)
	}
	var parts []string
	for _, tb := range []struct {
		icon, label string
		view        int
	}{
		{"\uf188", "FINDINGS", viewFindings},
		{"\uf07b", "FILES", viewFiles},
		{"\uf0e5", "COMMENTS", viewComments},
	} {
		if tb.view == m.leftView {
			parts = append(parts, act(tb.icon, tb.label))
		} else {
			parts = append(parts, dimTab(tb.icon, tb.label))
		}
	}
	return cut(strings.Join(parts, sep), w)
}

// wsBooting reports whether the tuicr pane has not drawn anything yet.
func wsBooting(lines []string) bool {
	for _, l := range lines {
		if strings.TrimSpace(stripANSIish(l)) != "" {
			return false
		}
	}
	return true
}

// wsBootingView renders the loader shown while tuicr starts up (it fetches
// the MR from GitLab, which can take a few seconds).
func (m *model) wsBootingView(w, h int) []string {
	msg := m.spin.View() + " starting tuicr… (fetches the MR from GitLab)"
	var out []string
	row := (h - 1) / 2
	padL := max(0, (w-lipgloss.Width(msg))/2)
	for i := 0; i < h; i++ {
		if i == row {
			out = append(out, strings.Repeat(" ", padL)+msg)
		} else {
			out = append(out, "")
		}
	}
	return out
}

func wsPlaceholder(w, h int) []string {
	var out []string
	msg := []string{"", "tuicr exited.", "Press W to reopen, q to quit the workspace."}
	padTop := (h - len(msg)) / 2
	if padTop < 0 {
		padTop = 0
	}
	for i := 0; i < padTop; i++ {
		out = append(out, "")
	}
	for _, l := range msg {
		out = append(out, styleDim.Render(l))
	}
	return out
}

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// ansiLines splits an ANSI-rendered screen into lines.
func ansiLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// ---- input -----------------------------------------------------------------

// wsHandleKey routes keys in workspace mode. Returns (handled, cmd).
func (m *model) wsHandleKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+g" || k == "ctrl+o" {
		if wsFocus == wsFocusTuicr {
			wsFocus = wsFocusCockpit
		} else {
			wsFocus = wsFocusTuicr
		}
		return true, nil
	}
	if wsFocus == wsFocusTuicr && wsPane != nil && !wsExited {
		if b := keyBytes(msg); len(b) > 0 {
			wsPane.Write(b)
		}
		return true, nil
	}
	// cockpit focus
	switch k {
	case "W":
		m.closeTuicrPane()
		return true, nil
	case "tab":
		wsFocus = wsFocusTuicr
		return true, nil
	case "esc":
		if wsDetail {
			wsDetail = false // back to the findings list
			wsDetailTop = 0
			m.wsResize() // restore the 40/60 split
			return true, nil
		}
		if m.leftView == viewFileFindings {
			m.setView(viewFiles) // pop the submenu back to FILES
			return true, nil
		}
	case "i", "enter":
		if !wsDetail && m.current() != nil {
			wsDetail = true // detail inside the cockpit strip
			wsDetailTop = 0
			m.wsResize() // cockpit expands, tuicr reflows
			return true, nil
		}
	case "j", "down":
		if wsDetail {
			m.wsDetailScroll(1)
			return true, nil
		}
	case "k", "up":
		if wsDetail {
			m.wsDetailScroll(-1)
			return true, nil
		}
	}
	// passthrough: keys the cockpit does not claim (tuicr's m/M comment
	// keys, c/V/? …) go straight to the embedded tuicr — no focus juggling.
	// Bound keys (j/k, a/r/e/d, 1/2/3, :, /, W, Tab …) stay with the cockpit.
	if len(k) == 1 && m.lookup(k) == nil {
		wsPane.WriteString(k)
		return true, nil
	}
	return false, nil
}

// wsHandleMouse forwards mouse events over the tuicr pane as SGR sequences
// (coordinates relative to the pane, 1-based). Clicks also move focus.
func (m *model) wsHandleMouse(mmsg tea.MouseMsg) (bool, tea.Cmd) {
	if wsPane == nil || wsExited {
		return false, nil
	}
	mo := mmsg.Mouse()
	cw := m.wsCockpitWidth()
	release := false
	switch mmsg.(type) {
	case tea.MouseReleaseMsg:
		release = true
	case tea.MouseMotionMsg:
	}
	lx := mo.X - (cw + 2) // cockpit box + 1 gap + 1 border
	ly := mo.Y - 3        // 2 header rows + 1 top border
	w, h := m.wsPaneSize()
	if lx < 0 || ly < 0 || lx >= w || ly >= h {
		return false, nil // cockpit area: normal handling
	}
	if wsFocus != wsFocusTuicr {
		wsFocus = wsFocusTuicr // click-to-focus
	}
	if b := sgrMouse(mo, lx, ly, release); b != "" {
		wsPane.WriteString(b)
	}
	return true, nil
}

// sgrMouse encodes a mouse event as an SGR sequence relative to the pane
// (1-based coordinates), mirroring xterm's 1006 mode.
func sgrMouse(mo tea.Mouse, lx, ly int, release bool) string {
	var code int
	switch mo.Button {
	case tea.MouseLeft:
		code = 0
	case tea.MouseMiddle:
		code = 1
	case tea.MouseRight:
		code = 2
	case tea.MouseWheelUp:
		code = 64
	case tea.MouseWheelDown:
		code = 65
	case tea.MouseNone:
		code = 3 // motion without button
	default:
		return "" // unsupported button: drop
	}
	if mo.Mod.Contains(tea.ModShift) {
		code += 4
	}
	if mo.Mod.Contains(tea.ModAlt) {
		code += 8
	}
	if mo.Mod.Contains(tea.ModCtrl) {
		code += 16
	}
	term := "M"
	if release {
		term = "m"
	}
	if mo.Button == tea.MouseNone {
		code = 32 // motion report
	}
	return fmt.Sprintf("\x1b[<%d;%d;%d%s", code, lx+1, ly+1, term)
}

// keyBytes converts a key press to the byte sequence a child PTY expects.
func keyBytes(msg tea.KeyPressMsg) []byte {
	switch msg.String() {
	case "enter":
		return []byte("\r")
	case "esc":
		return []byte("\x1b")
	case "backspace":
		return []byte("\x7f")
	case "tab":
		return []byte("\t")
	case "shift+tab":
		return []byte("\x1b[Z")
	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")
	case "home":
		return []byte("\x1b[H")
	case "end":
		return []byte("\x1b[F")
	case "pgup":
		return []byte("\x1b[5~")
	case "pgdown":
		return []byte("\x1b[6~")
	case "delete":
		return []byte("\x1b[3~")
	case "space":
		return []byte(" ")
	}
	if len(msg.String()) > 5 && strings.HasPrefix(msg.String(), "ctrl+") {
		c := msg.String()[5]
		if c >= 'a' && c <= 'z' {
			return []byte{c & 0x1f}
		}
	}
	if msg.Text != "" {
		return []byte(msg.Text)
	}
	if msg.Code >= 0x20 && msg.Code < 0x7f { // printable rune with empty Text
		return []byte{byte(msg.Code)}
	}
	return nil
}

// wsResize propagates terminal resizes to the pane.
func (m *model) wsResize() {
	if wsPane == nil {
		return
	}
	w, h := m.wsPaneSize()
	wsPane.Resize(w, h)
}
