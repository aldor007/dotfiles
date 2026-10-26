package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"mr-review/internal/store"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// Detail tabs (right pane).
// Chunk 2 (§2): collapsed from 5 → 2. Overview composes diff +
// evidence + explanation + discussion summary; Comment is the editor.
const (
	tabOverview = iota
	tabComment
	tabCount
)

var tabNames = []string{"Overview", "Comment"}

// Left-pane views (findings | files | comments | file's findings).
const (
	viewFindings = iota
	viewFiles
	viewComments
	viewFileFindings
	viewCount
)

// Overlay screens. The overlay field mirrors the top of the overlayStack
// (see pushOverlay/popOverlay); ovPalette was appended after ovExport so
// the pre-existing iota values (ovPublish == 4, asserted by tests) do not
// shift.
const (
	ovNone = iota
	ovSummary
	ovFilter
	ovHelp
	ovPublish
	ovInspector
	ovExport
	ovPalette
	ovQueue
	ovViewer
)

// Sort modes for the findings list.
const (
	sortSeverity = iota
	sortFile
	sortDecision
)

// inputMode identifies the active input mode for key routing. Review
// shortcuts never fire while a text mode is active (input safety).
type inputMode int

const (
	modeNormal inputMode = iota
	modePalette
	modeSearch
	modeDraftEdit
	modeExport
	modePublish
	modeOverlay
	modeFindingEdit
	modeAsk
	modeConfirm
)

func promptPath(dir string) string { return dir + "/prompt.txt" }
func logPath(dir string) string    { return dir + "/agent.log" }

type spinnerTickMsg struct{}

func spinTick(m *model) tea.Cmd {
	interval := 500 * time.Millisecond
	if m.bgRunning() {
		interval = 150 * time.Millisecond // visible spinner for the bg review
	}
	return tea.Batch(m.spin.Tick, tea.Tick(interval,
		func(time.Time) tea.Msg { return spinnerTickMsg{} }))
}

// RunWorkspace starts the review with the tuicr pane open (hybrid workspace).
func RunWorkspace(dir string) error {
	rev, err := store.LoadReview(dir)
	if err != nil {
		return err
	}
	findings, err := store.LoadFindings(dir)
	if err != nil {
		return err
	}
	ti := textinput.New()
	ti.Placeholder = "ask the agent about this finding…"
	ti.CharLimit = 500
	ti.Prompt = "ask> "

	ta := textarea.New()
	ta.Placeholder = "The GitLab comment text…"
	ta.MaxHeight = 12

	helpF := textinput.New()
	helpF.Prompt = "/ "
	helpF.Placeholder = "filter… (esc clears)"
	search := textinput.New()
	search.Prompt = "/ "

	pal := textinput.New()
	pal.Prompt = ": "

	comments, _ := store.LoadComments(dir)

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))

	m := &model{
		dir: dir, rev: rev, findings: findings, comments: comments,
		filter: Filter{Kind: "all"}, input: ti, commentTA: ta, spin: sp,
		ctxLines: 6, searchInput: search, paletteInput: pal, helpFilterInput: helpF,
		visualStart: -1, wsAutoOpen: true,
	}
	m.rebuild()
	m.startDiffFirst()
	p := tea.NewProgram(m)
	// P1-1 guaranteed teardown: whatever way p.Run() ends (q, esc, ctrl+c,
	// window close, fatal error), the embedded tuicr pane is closed here —
	// the child process and its PTY can never outlive the host UI.
	defer m.closeTuicrPane()
	_, err = p.Run()
	return err
}

// Run starts the interactive review screen for a review dir.
func Run(dir string) error {
	rev, err := store.LoadReview(dir)
	if err != nil {
		return err
	}
	findings, err := store.LoadFindings(dir)
	if err != nil {
		return err
	}
	ti := textinput.New()
	ti.Placeholder = "ask the agent about this finding…"
	ti.CharLimit = 500
	ti.Prompt = "ask> "

	ta := textarea.New()
	ta.Placeholder = "The GitLab comment text…"
	ta.MaxHeight = 12

	search := textinput.New()
	search.Placeholder = "search…  (enter keep · esc clear)"
	search.Prompt = "/ "

	helpF := textinput.New()
	helpF.Prompt = "/ "
	helpF.Placeholder = "filter… (esc clears)"

	pal := textinput.New()
	pal.Placeholder = "type a command…"
	pal.Prompt = ": "
	pal.CharLimit = 100

	comments, _ := store.LoadComments(dir) // drafts may not exist yet: fine

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))

	m := &model{
		dir: dir, rev: rev, findings: findings, comments: comments,
		filter: Filter{Kind: "all"}, input: ti, commentTA: ta, spin: sp,
		ctxLines: 6, searchInput: search, paletteInput: pal, helpFilterInput: helpF,
		visualStart: -1,
	}
	m.rebuild()
	m.startDiffFirst()
	p := tea.NewProgram(m)
	// P1-1 guaranteed teardown (the pane CAN exist here: the W registry
	// action is not gated on workspace mode): same contract as RunWorkspace.
	defer m.closeTuicrPane()
	_, err = p.Run()
	return err
}

// ---- messages --------------------------------------------------------------

type discussDoneMsg struct {
	id     string
	answer string
	err    error
}

type childDoneMsg struct {
	err  error
	note string
}

type fileChangedMsg struct{} // state on disk changed; reload

// ---- model -----------------------------------------------------------------

type model struct {
	dir            string
	rev            *store.Review
	findings       []store.Finding
	filtered       []int
	cursor         int
	filter         Filter
	sortMode       int
	msg            string
	overlay        int   // top-of-stack overlay id (ovNone on an empty stack)
	overlayStack   []int // focus-stack of open overlay screens (bottom → top)
	input          textinput.Model
	inputOn        bool
	commentTA      textarea.Model
	editingComment bool
	spin           spinner.Model
	busy           bool
	busySince      time.Time
	confirmClean   bool
	// background AI ask (non-blocking; Esc cancels)
	askingID    string
	askCancel   context.CancelFunc
	askingSince time.Time
	width       int
	height      int

	// layout / navigation
	focus     int // focusList | focusDetail
	tab       int // detail tab index
	detailTop map[int]int
	listTop   int
	narrow    bool // width < 90: single-pane mode
	showList  bool // narrow mode: showing the list (vs detail)
	leftView  int  // viewFindings | viewFiles | viewComments | viewFileFindings
	zoom      bool // detail pane fills the screen

	// fileFindingsFile is the file whose findings are shown in viewFileFindings;
	// set automatically when entering FILES view + Enter on a file (non-ws).
	fileFindingsFile string
	fileFindCur      int

	// continuous-diff browsing (manual review mode)
	browseFile     string // non-empty: browsing (the file under the cursor)
	diffCursor     int    // row index into the continuous all-MR document
	visualOn       bool   // range selection for a range comment
	visualStart    int    // selection anchor row
	browseDocCache []browseRow
	browseDocVer   int
	diffVersion    int

	// pending multi-key sequence ("gg", "gd", "gn", …)
	pendingKey string

	// mouse drag state (cockpit mouse: click/drag/release copy in the diff)
	mouse mousePress

	// diff view
	diffParsed map[string]*FileDiff
	ctxLines   int
	diffHOff   int // horizontal scroll offset (cells) for the Diff tab

	// side-by-side diff mode (session-scoped; rendered by diffmode.go).
	// Only ACTIVE when the diff pane is wide enough — see sideBySideActive().
	diffSideBySide       bool
	gutterWCache         int // cached widest line-number column (per gutterWVer)
	gutterWVer           int
	browseSeeded         bool // render-time first-file seed has run (runs once)
	viewerTitle          string
	viewerLines          []string
	viewerTop            int
	overlaySavedFocus    int // pane context saved at the first pushOverlay
	overlaySavedLeftView int
	overlaySavedValid    bool // direct m.overlay= assignments must not restore stale zeros

	// changed-file tree + comment navigator state
	tree         *fileTree
	treeCursor   int
	comments     []store.DraftComment
	commentCur   int
	remoteNotes  []remoteNote
	remoteLoaded bool

	// hybrid workspace (embedded tuicr pane)
	wsAutoOpen bool

	// background AI review heartbeat (agent.state)
	bgReview      *store.AgentState
	bgRaw         int
	bgLoaded      bool   // findings from a finished bg review are in memory
	bgActivity    string // last agent.log line (live progress hint)
	pendingReload bool   // reload deferred while an editor is open

	// command palette / search
	paletteOn    bool
	paletteInput textinput.Model
	paletteSel   int
	paletteItems []Action
	searchOn     bool
	searchInput  textinput.Model
	searchText   string

	// help-overlay search: a live filter inside the keyboard reference
	helpFilterOn    bool
	helpFilterInput textinput.Model
	helpFilter      string
	helpTop         int // help overlay scroll offset (j/k/Ctrl-d/u)

	// overlays with rich state
	inspectorTop  int
	draftEdit     *draftEditor  // manual comment editor overlay
	exportDlg     *exportDialog // export dialog overlay
	confirmDelete bool          // y/n: delete the selected draft

	pubBin string // publisher binary path (tests may override)
}

func (m *model) rebuild() {
	m.filtered = m.filtered[:0]
	for i := range m.findings {
		if m.filter.Match(&m.findings[i]) {
			m.filtered = append(m.filtered, i)
		}
	}
	sortFindingsMode(m.findings, m.filtered, m.sortMode)
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampScrolls()
}

func (m *model) current() *store.Finding {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	return &m.findings[m.filtered[m.cursor]]
}

// setDecision persists a review decision, then auto-advances to the next
// pending finding. The status message always refers to the finding that is
// actually selected afterwards (stable IDs, never raw indices).
func (m *model) setDecision(d string) tea.Cmd {
	f := m.current()
	if f == nil {
		return nil
	}
	id := f.ID // stable ID survives re-sorting/re-filtering
	f.Decision = d
	// Persist through a store transaction (read → mutate → save under the
	// advisory lock), matching the STORED finding by stable ID. A blind
	// whole-file write of m.findings would clobber whatever the background
	// AI worker saved since the TUI loaded (review P1: lost decisions),
	// and could itself be overwritten by the worker's save.
	if err := store.UpdateFindings(m.dir, func(fs []store.Finding) ([]store.Finding, error) {
		for i := range fs {
			if fs[i].ID == id {
				fs[i].Decision = d
				break
			}
		}
		return fs, nil
	}); err != nil {
		m.msg = "save failed: " + err.Error()
		return nil
	}
	verb := map[string]string{"approved": "approved", "rejected": "rejected", "pending": "undid decision on"}[d]
	if verb == "" {
		verb = d
	}
	m.msg = fmt.Sprintf("%s %s", id, verb)

	if d != "approved" && d != "rejected" {
		// undo: stay on the same finding
		m.reselect(id)
		return nil
	}

	// decision sort reorders the list — keep the acted-on finding selected
	if m.sortMode == sortDecision {
		m.reselect(id)
	}

	// auto-advance to the next pending finding (skips published & decided)
	next := ""
	for step := 1; step <= len(m.filtered); step++ {
		idx := (m.cursor + step) % len(m.filtered)
		nf := &m.findings[m.filtered[idx]]
		if nf.ID == id {
			break // wrapped all the way around: nothing else pending
		}
		if nf.Decision == "pending" && nf.Publication == nil && !nf.Stale {
			m.cursor = idx
			next = nf.ID
			break
		}
	}
	if next != "" {
		m.msg += fmt.Sprintf(" → next pending %s", next)
	} else {
		_, pe, _, _ := m.decisionCounts()
		if pe == 0 {
			m.msg += " — review complete, no pending findings (P to publish)"
		}
	}
	m.syncDiffScroll()
	return nil
}

// currentID returns the stable ID of the selected finding ("" when none).
func (m *model) currentID() string {
	if f := m.current(); f != nil {
		return f.ID
	}
	return ""
}

// reselect moves the cursor to the finding with the given ID, keeping the
// selection stable across filter/sort changes; falls back to the nearest
// visible finding.
func (m *model) reselect(id string) {
	for i, idx := range m.filtered {
		if m.findings[idx].ID == id {
			m.cursor = i
			m.syncDiffScroll()
			return
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.syncDiffScroll()
}

// ---- init/update -----------------------------------------------------------

func (m *model) Init() tea.Cmd {
	if m.wsAutoOpen {
		return tea.Batch(textinput.Blink, bgTick(), m.openTuicrPane())
	}
	return tea.Batch(textinput.Blink, bgTick())
}

// ---- modal stack -----------------------------------------------------------

// pushOverlay opens an overlay screen. The id goes on top of the overlay
// stack (vi-SQL peeker focus stack: Esc pops exactly one level and restores
// the previous context) and the overlay field mirrors the new top. Keeping
// overlay a plain int field means direct assignments (tests, registry
// actions) keep working: with an empty stack the field itself is the
// current modal.
func (m *model) pushOverlay(id int) {
	// Entering the first overlay saves the pane context: overlays (Enter →
	// floating finding view) may legitimately move focus while open, but
	// closing must return the user to the pane they were in.
	if len(m.overlayStack) == 0 {
		m.overlaySavedFocus = m.focus
		m.overlaySavedLeftView = m.leftView
		m.overlaySavedValid = true
	}
	m.overlayStack = append(m.overlayStack, id)
	m.overlay = id
}

// popOverlay closes the top overlay screen (Esc): one level pops, and the
// overlay field returns to the previous top (ovNone when the stack empties).
// Overlays never mutate navigation state, so restoring the previous top is
// restoring the exact previous focus/context.
func (m *model) popOverlay() {
	if n := len(m.overlayStack); n > 0 {
		m.overlayStack = m.overlayStack[:n-1]
	}
	if n := len(m.overlayStack); n > 0 {
		m.overlay = m.overlayStack[n-1]
	} else {
		m.overlay = ovNone
		// last overlay closed: return to the pane the user was in before the
		// first overlay opened (Enter's floating view moves focus while open).
		// Only pushOverlay saves context — direct m.overlay= assignments
		// (registry actions) never had a save, so never restore stale zeros.
		if m.overlaySavedValid {
			m.focus = m.overlaySavedFocus
			m.leftView = m.overlaySavedLeftView
			m.overlaySavedValid = false
			m.clampScrolls()
		}
	}
}

// topOverlay returns the current overlay id: the stack top when screens
// were pushed via pushOverlay, else the overlay field (which mirrors the
// top and also absorbs direct assignments). View() renders through this so
// the active modal is always the stack top.
func (m *model) topOverlay() int {
	if n := len(m.overlayStack); n > 0 {
		return m.overlayStack[n-1]
	}
	return m.overlay
}

// curMode reports the active input mode; normal-mode review shortcuts only
// fire in modeNormal (input safety).
func (m *model) curMode() inputMode {
	switch {
	case m.paletteOn:
		return modePalette
	case m.searchOn:
		return modeSearch
	case m.draftEdit != nil:
		return modeDraftEdit
	case m.exportDlg != nil:
		return modeExport
	case m.overlay == ovPublish:
		return modePublish
	case m.overlay != ovNone:
		return modeOverlay
	case m.editingComment:
		return modeFindingEdit
	case m.inputOn:
		return modeAsk
	case m.confirmClean || m.confirmDelete:
		return modeConfirm
	}
	return modeNormal
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.narrow = m.width < 80
		m.clampScrolls()
		m.wsResize()
		// force a full repaint: after a terminal resize the diff-based
		// renderer can leave stale cells outside the old frame (observed as
		// ghost content in a 120-col grid after an 80-col start)
		return m, tea.ClearScreen

	case bgTickMsg:
		if m.bgRunning() {
			m.bgActivity = bgLastActivity(m.dir)
		}
		if reload := m.refreshBGState(); reload {
			m.bgLoaded = true
			if m.editingComment || m.draftEdit != nil {
				m.pendingReload = true // applied when the editor closes
				m.msg = "AI review finished — findings load after you close the editor"
				return m, m.bgPoll()
			}
			if err := m.reload(); err != nil {
				m.msg = "reload failed: " + err.Error()
			} else {
				m.msg = "AI review finished — findings loaded"
			}
		}
		return m, m.bgPoll()

	case tuicrDoneMsg:
		m.handleTuicrDone(msg)
		return m, nil

	case discussDoneMsg:
		m.busy = false
		m.askingID = ""
		m.askCancel = nil
		switch {
		case msg.err == nil:
			m.msg = "answer recorded on " + msg.id
		case errors.Is(msg.err, context.Canceled):
			m.msg = "AI ask cancelled — nothing recorded"
		default:
			m.msg = "discuss failed: " + msg.err.Error()
		}
		return m, m.reloadCmd()

	case childDoneMsg:
		m.busy = false
		m.msg = msg.note
		if msg.err != nil {
			m.msg = msg.note + ": " + msg.err.Error()
		}
		return m, m.reloadCmd()

	case fileChangedMsg:
		if err := m.reload(); err != nil {
			m.msg = "reload failed: " + err.Error()
		}
		return m, nil

	case spinnerTickMsg:
		if m.busy || m.askingID != "" || m.bgRunning() {
			return m, spinTick(m)
		}
		return m, nil

	case wsDataMsg:
		// pane screen changed: the next View picks it up; keep listening
		return m, m.wsDataPump()

	case wsBootTickMsg:
		if wsPane != nil && !wsExited && wsBooting(ansiLines(wsPane.Render())) {
			return m, wsBootTick() // keep the loader spinning until tuicr draws
		}
		return m, nil

	case wsPaneExitMsg:
		if wsPane != nil {
			wsExited = true
			m.msg = "tuicr exited — W reopens, q quits the workspace"
		}
		return m, nil

	case wsPrepareDoneMsg:
		// async PrepareSession result (review fix P2 #10): updates the
		// status line without blocking the TUI. msg.status matches
		// tuicr.SessionStatus (Missing/Ready/Partial); we use string
		// comparison to avoid pulling the tuicr package into this file.
		switch {
		case msg.err != nil:
			m.msg = "tuicr session check failed: " + msg.err.Error()
		case int(msg.status) == 2: // tuicr.SessionStatusPartial
			m.msg = "tuicr session file count is below the MR diff — open it to inspect; do not auto-clear"
		case msg.slug != "":
			m.msg = ""
		}
		return m, nil

	case tea.MouseMsg:
		if wsPane != nil {
			if handled, cmd := m.wsHandleMouse(msg); handled {
				return m, cmd
			}
		}
		return m, m.handleCockpitMouse(msg)

	case tea.KeyPressMsg:
		// Ctrl+C always quits, even mid-operation (an in-flight AI ask is
		// cancelled first so the agent process is not left running).
		if msg.String() == "ctrl+c" {
			if m.askCancel != nil {
				m.askCancel()
			}
			// P1-1: close the embedded tuicr pane BEFORE quitting so the
			// child process and its PTY die with the UI (the defer in
			// Run/RunWorkspace backstops this for every other exit path).
			m.closeTuicrPane()
			return m, tea.Quit
		}
		if m.askingID != "" && msg.String() == "esc" {
			// cancel the in-flight AI ask (the status bar promises this)
			if m.askCancel != nil {
				m.askCancel()
			}
			m.askingID = ""
			m.askCancel = nil
			m.msg = "cancelling the AI ask…"
			return m, nil
		}
		if m.busy {
			return m, nil
		}
		// drop key events carrying control/escape bytes — typed text never
		// contains them, but mis-parsed terminal query responses (OSC 11 bg
		// color, DA1, …) leak as runes and would poison focused inputs
		// (palette filter, search, editors). See TestSanitizeKeyEvents.
		for _, r := range msg.Text {
			if r < 0x20 || (r >= 0x7f && r <= 0xa0) {
				return m, nil
			}
		}
		if wsPane != nil {
			if handled, cmd := m.wsHandleKey(msg); handled {
				return m, cmd
			}
		}
		switch m.curMode() {
		case modePalette:
			return m.updatePalette(msg)
		case modeSearch:
			return m.updateSearch(msg)
		case modeDraftEdit:
			return m.updateDraftEditor(msg)
		case modeExport:
			return m.updateExportDialog(msg)
		case modePublish: // publish preview: Enter confirms, Esc cancels
			switch msg.String() {
			case "enter":
				m.popOverlay()
				return m, m.publishRun()
			case "esc", "q", "n":
				m.popOverlay()
				m.msg = "publish cancelled — nothing was sent"
				return m, nil
			}
			return m, nil
		case modeOverlay:
			return m.updateOverlayKey(msg)
		case modeFindingEdit:
			return m.updateCommentEditor(msg)
		case modeAsk:
			return m.updateInput(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

// updateConfirm handles y/n confirmations (worktree cleanup, draft delete).
func (m *model) updateConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.confirmDelete {
		switch strings.ToLower(msg.String()) {
		case "y":
			m.confirmDelete = false
			m.deleteSelectedDraft()
			return m, nil
		case "n", "esc":
			m.confirmDelete = false
			m.msg = "draft kept"
			return m, nil
		}
		return m, nil
	}
	switch strings.ToLower(msg.String()) {
	case "y":
		m.confirmClean = false
		return m, m.cleanupCmd()
	case "n", "esc":
		m.confirmClean = false
		m.msg = "worktree kept"
		return m, nil
	}
	return m, nil
}

// updateOverlayKey routes keys for the simple overlays (filter, help,
// summary, inspector).
func (m *model) updateOverlayKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch m.overlay {
	case ovFilter:
		if n := keyDigit(msg); n > 0 {
			m.applyFilterChoice(n)
			return m, nil
		}
		if k == "esc" || k == "q" {
			m.popOverlay()
			return m, nil
		}
	case ovInspector:
		// full finding actions INSIDE the floating window; esc/q/i exit
		switch k {
		case "esc", "q", "i":
			m.popOverlay()
			return m, nil
		case "j", "down":
			m.findingsDetailsScroll(2)
			return m, nil
		case "k", "up":
			m.findingsDetailsScroll(-2)
			return m, nil
		case "ctrl+d":
			m.findingsDetailsScroll(12)
			return m, nil
		case "ctrl+u":
			m.findingsDetailsScroll(-12)
			return m, nil
		case "a", "r", "u":
			id := ""
			if f := m.current(); f != nil {
				id = f.ID // stay on this finding inside the window
			}
			var cmd tea.Cmd
			switch k {
			case "a":
				cmd = m.setDecision("approved")
			case "r":
				cmd = m.setDecision("rejected")
			default:
				cmd = m.setDecision("pending")
			}
			m.reselect(id) // cancel the auto-advance: the window stays put
			return m, cmd
		case "e":
			m.popOverlay()
			return m, m.startInlineCommentEdit()
		case "y":
			return m, m.runCopy()
		case "d":
			m.popOverlay()
			return m, m.startDiscuss()
		case "S":
			m.popOverlay()
			return m, m.stageTuicrCmd()
		}
		return m, nil
	case ovHelp:
		if k == "/" {
			m.helpFilterOn = !m.helpFilterOn
			if m.helpFilterOn {
				m.helpFilterInput.Focus()
			} else {
				m.helpFilterInput.Blur()
			}
			return m, nil
		}
		if k == "esc" {
			if m.helpFilterOn {
				m.helpFilterOn = false
				m.helpFilterInput.Blur()
				m.helpFilter = ""
				return m, nil
			}
			m.popOverlay()
			return m, nil
		}
		if k == "?" { // ? closes too (help floats; underlying focus untouched)
			m.popOverlay()
			return m, nil
		}
		if m.helpFilterOn {
			var cmd tea.Cmd
			m.helpFilterInput, cmd = m.helpFilterInput.Update(msg)
			m.helpFilter = m.helpFilterInput.Value()
			return m, cmd
		}
		switch k { // scroll the reference when it exceeds the box
		case "j", "down":
			m.helpScrollTo(1)
			return m, nil
		case "k", "up":
			m.helpScrollTo(-1)
			return m, nil
		case "ctrl+d":
			m.helpScrollTo(m.helpWindow())
			return m, nil
		case "ctrl+u":
			m.helpScrollTo(-m.helpWindow())
			return m, nil
		}
		m.popOverlay() // any other key closes
		return m, nil
	default: // summary
		if k == "esc" || k == "q" || (k == "s" && msg.Text != "?") {
			m.popOverlay()
			return m, nil
		}
	case ovViewer:
		switch k {
		case "esc", "q", "i":
			m.popOverlay()
			return m, nil
		case "j", "down":
			m.viewerTop++
		case "k", "up":
			if m.viewerTop > 0 {
				m.viewerTop--
			}
		case "ctrl+d":
			m.viewerTop += 15
		case "ctrl+u":
			m.viewerTop -= 15
		}
		return m, nil
	case ovQueue:
		if k == "esc" || k == "q" || k == "Q" {
			m.popOverlay()
			return m, nil
		}
		return m, nil // read-only: keys do nothing else while the queue is open
	}
	return m, nil
}

// updateNormal routes a normal-mode key: esc context-cancels, everything
// else goes through the action registry.
func (m *model) updateNormal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "esc" {
		switch {
		case m.pendingKey != "":
			m.pendingKey = ""
			return m, nil
		case m.visualOn:
			m.cancelVisual()
			m.msg = "selection cancelled"
			return m, nil
		case m.searchText != "":
			m.searchText = ""
			m.applyViewFilters()
			m.msg = "search cleared"
			return m, nil
		case m.browseFile != "":
			m.exitBrowse()
			return m, nil
		case m.zoom:
			m.zoom = false
			m.clampScrolls()
			return m, nil
		case m.narrow && m.focus == focusDiff:
			m.focus = focusFindings
			return m, nil
		case m.focus == focusFindings && m.leftView != viewFindings:
			m.setView(viewFindings)
			return m, nil
		}
		// P1-1: Esc-quit closes the tuicr pane before the program exits.
		m.closeTuicrPane()
		return m, tea.Quit
	}
	return m, m.handleKey(k)
}

func keyDigit(msg tea.KeyPressMsg) int {
	if len(msg.String()) == 1 && msg.String()[0] >= '1' && msg.String()[0] <= '9' {
		return int(msg.String()[0] - '0')
	}
	return 0
}

func (m *model) updateInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.inputOn = false
		m.input.Reset()
		m.msg = ""
		return m, nil
	case "enter":
		q := strings.TrimSpace(m.input.Value())
		m.inputOn = false
		m.input.Reset()
		if q == "" {
			m.msg = ""
			return m, nil
		}
		f := m.current()
		if f == nil {
			return m, nil
		}
		// non-blocking background ask: the user keeps triaging while the
		// agent runs; Esc cancels (see Update), the timeout is enforced in
		// runner.Discuss. m.busy stays false — it is reserved for
		// ExecProcess handoffs (publish/refresh/nvim).
		id := f.ID
		dir := m.dir
		question := q
		ctx, cancel := context.WithCancel(bgCtx())
		m.askingID = id
		m.askCancel = cancel
		m.askingSince = time.Now()
		m.msg = "asking the AI about " + id + "…"
		return m, tea.Batch(
			func() tea.Msg {
				ans, err := newRunner().Discuss(ctx, dir, id, question, discussTimeout())
				return discussDoneMsg{id: id, answer: ans, err: err}
			},
			spinTick(m),
		)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// (the former updateKey switch was replaced by the action registry — see
// registry.go; jumpHunk below remains as the findings-mode hunk jump.)

// jumpHunk moves the diff viewport to the previous/next hunk header in the
// focused file's diff; expands context when the edge is reached.
func (m *model) jumpHunk(dir int) {
	f := m.current()
	if f == nil {
		return
	}
	fd := m.diffFor(f)
	if fd == nil {
		return
	}
	_, hl := DiffWindow(fd, f.Side, f.Line, f.LineEnd, m.ctxLines)
	absIdx := m.detailTop[tabOverview] + hl // approximate absolute position
	if dir < 0 {
		// nearest header strictly above the current window
		best := -1
		for i, l := range fd.Lines {
			if l.Kind == DiffHunkHeader && i < absIdx-1 {
				best = i
			}
		}
		if best >= 0 {
			m.detailTop[tabOverview] = max(0, best-1)
			return
		}
		if m.ctxLines < 200 {
			m.ctxLines += 12
		}
		m.detailTop[tabOverview] = 0
		return
	}
	for i, l := range fd.Lines {
		if l.Kind == DiffHunkHeader && i > absIdx+1 {
			m.detailTop[tabOverview] = max(0, i-1)
			return
		}
	}
	// no further hunk: expand context to reveal more
	if m.ctxLines < 200 {
		m.ctxLines += 12
	}
	m.clampScrolls()
}

// applyFilterChoice applies a numeric filter-menu choice.
func (m *model) applyFilterChoice(n int) {
	opts := filterOptions()
	if n-1 < len(opts) {
		m.filter = opts[n-1].f
		id := m.currentID() // keep the selection stable when still visible
		m.rebuild()
		m.reselect(id)
		m.msg = "filter: " + m.filter.Describe()
	}
	m.popOverlay()
}

func (m *model) reload() error {
	if m.editingComment || m.draftEdit != nil {
		// an editor holds a pointer into the old slice — defer the swap
		m.pendingReload = true
		return nil
	}
	fs, err := store.LoadFindings(m.dir)
	if err != nil {
		return err
	}
	rev, err := store.LoadReview(m.dir)
	if err != nil {
		return err
	}
	comments, err := store.LoadComments(m.dir)
	if err != nil {
		return err
	}
	m.rev, m.comments = rev, comments
	m.remoteLoaded = false                              // remote notes may have changed too
	m.ingestFindings(reconcileFindings(m.findings, fs)) // ID-stable selection across the findings swap
	m.rebuild()
	return nil
}

// reconcileFindings merges an incoming findings slice (freshly loaded from
// disk — possibly rewritten by the background AI worker or the publisher)
// with the human state the TUI currently holds, keyed by stable finding ID
// (review P1: a re-ingested AI snapshot must not wipe human decisions).
//
// Rules, per finding that exists on BOTH sides:
//   - AI/content fields always come from the incoming record (it is the
//     fresher copy),
//   - a non-default human value on EITHER side survives: a decision,
//     publication, edited comment, discussion, or stale flag the incoming
//     record lost (a racing worker's stale snapshot) is restored from the
//     stored record, while non-default values the incoming record DOES
//     carry win — they were written deliberately on disk (publisher
//     marking Publication, a refresh carrying decisions, another save).
//
// A finding with an unknown ID is a genuinely new AI finding: taken as-is.
func reconcileFindings(stored, incoming []store.Finding) []store.Finding {
	if len(stored) == 0 || len(incoming) == 0 {
		return incoming
	}
	prev := make(map[string]*store.Finding, len(stored))
	for i := range stored {
		prev[stored[i].ID] = &stored[i]
	}
	out := make([]store.Finding, len(incoming))
	for i := range incoming {
		nf := incoming[i]
		if of, ok := prev[nf.ID]; ok {
			if nf.Decision == "pending" && of.Decision != "pending" {
				nf.Decision = of.Decision
			}
			if nf.Publication == nil {
				nf.Publication = of.Publication
			}
			if strings.TrimSpace(nf.EditedComment) == "" {
				nf.EditedComment = of.EditedComment
			}
			if len(nf.Discussion) == 0 {
				nf.Discussion = append([]store.DiscussionTurn(nil), of.Discussion...)
			}
			if of.Stale {
				nf.Stale = true
			}
		}
		out[i] = nf
	}
	return out
}

func (m *model) reloadCmd() tea.Cmd {
	return func() tea.Msg { return fileChangedMsg{} }
}

// startInlineCommentEdit opens the inline textarea with the current comment.
func (m *model) startInlineCommentEdit() tea.Cmd {
	f := m.current()
	if f == nil {
		return nil
	}
	m.editingComment = true
	m.tab = tabComment
	m.commentTA.SetValue(f.EffectiveComment())
	return m.commentTA.Focus()
}

// updateCommentEditor routes keys to the textarea; Ctrl+S saves, Esc cancels.
func (m *model) updateCommentEditor(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		f := m.current()
		if f == nil {
			m.editingComment = false
			return m, nil
		}
		id := f.ID
		f.EditedComment = strings.TrimSpace(m.commentTA.Value())
		// Same transactional save as setDecision: mutate the STORED finding
		// by stable ID under the advisory lock so a racing AI-worker write
		// cannot clobber the edit (and vice versa).
		if err := store.UpdateFindings(m.dir, func(fs []store.Finding) ([]store.Finding, error) {
			for i := range fs {
				if fs[i].ID == id {
					fs[i].EditedComment = f.EditedComment
					break
				}
			}
			return fs, nil
		}); err != nil {
			m.msg = "save failed: " + err.Error()
			return m, nil
		}
		m.editingComment = false
		m.msg = "comment saved on " + f.ID
		return m, m.bgPendingReload()
	case "esc":
		m.editingComment = false
		m.msg = "edit cancelled"
		return m, m.bgPendingReload()
	}
	var cmd tea.Cmd
	m.commentTA, cmd = m.commentTA.Update(msg)
	return m, cmd
}
