// registry.go — unified action registry.
//
// Every user-visible action is declared exactly once, with its keyboard
// bindings, context requirements and handler. Keyboard dispatch, the command
// palette (`:`) and the help overlay (`?`) are all derived from this list, so
// they can never drift apart.
//
// Safety invariant: no action here ever talks to GitLab directly. Remote
// writes go through the publish-preview → publisher-binary flow with its own
// typed confirmation (see publishRun / publishPreviewCmd).
package tui

import (
	"sort"
	"strings"

	"mr-review/internal/store"
	"mr-review/internal/tui/widget"

	tea "charm.land/bubbletea/v2"
)

// Category groups actions in the palette and help overlay.
type Category string

const (
	CatNavigation Category = "Navigation"
	CatFindings   Category = "AI Findings"
	CatManual     Category = "Manual Review"
	CatComments   Category = "Comments"
	CatGitLab     Category = "GitLab"
	CatExport     Category = "Export"
	CatView       Category = "View"
	CatApp        Category = "Application"
)

var categoryOrder = []Category{
	CatFindings, CatManual, CatComments, CatNavigation,
	CatView, CatExport, CatGitLab, CatApp,
}

// Action is one registrable command.
type Action struct {
	ID   string   // stable identifier ("findings.approve")
	Name string   // palette label ("Review: Approve finding")
	Desc string   // one-line description for palette/help
	Cat  Category // palette group
	Keys []string // keyboard bindings ("a", "gg"); empty = palette-only
	When func(m *model) bool
	Run  func(m *model) tea.Cmd
}

// KeyLabel renders the bindings for palette/help display.
func (a Action) KeyLabel() string { return strings.Join(a.Keys, " ") }

// ---- context helpers -------------------------------------------------------

// Package-level context predicates for the registry's When fields.
func hasFinding(m *model) bool { return m.current() != nil }

// hasFindingWindow: the finding-window diff mode (not the continuous
// browse) — context adjustment only means something there.
func hasFindingWindow(m *model) bool { return hasFinding(m) && m.browseFile == "" }

func hasBrowse(m *model) bool { return m.browseFile != "" }

func hasDraftSel(m *model) bool {
	return inCommentsView(m) && m.selectedDraft() != nil
}

func hasCommentSel(m *model) bool {
	return m.leftView == viewComments && m.filteredCommentCount() > 0
}

func publishableCount(m *model) int {
	n := 0
	for i := range m.findings {
		if m.findings[i].Decision == "approved" && m.findings[i].Publication == nil && !m.findings[i].Stale {
			n++
		}
	}
	for i := range m.comments {
		if m.comments[i].State == store.CommentReady && m.comments[i].Publication == nil {
			n++
		}
	}
	return n
}

func hasPublishable(m *model) bool { return publishableCount(m) > 0 }

func canMarkReviewed(m *model) bool { return m.browseFile != "" || m.treeSelection() != "" }

// inCommentsView scopes the navigator-specific meanings of e and d so they
// never fight the finding-level bindings (context-sensitive keys, vi-SQL
// style: one meaning per view).
func inCommentsView(m *model) bool { return m.leftView == viewComments }

// ---- the registry ----------------------------------------------------------

// buildActions declares every action in display order. It is pure with
// respect to state: context sensitivity lives in When.
func buildActions(m *model) []Action {
	return []Action{
		// ---- AI findings ----
		{ID: "findings.approve", Cat: CatFindings, Name: "Approve finding",
			Desc: "approve the selected AI finding (local only)", Keys: []string{"a"},
			When: hasFinding, Run: func(m *model) tea.Cmd { return m.setDecision("approved") }},
		{ID: "findings.reject", Cat: CatFindings, Name: "Reject finding",
			Desc: "reject the selected AI finding (local only)", Keys: []string{"r"},
			When: hasFinding, Run: func(m *model) tea.Cmd { return m.setDecision("rejected") }},
		{ID: "findings.undo", Cat: CatFindings, Name: "Undo decision",
			Desc: "return the finding to pending", Keys: []string{"u"},
			When: hasFinding, Run: func(m *model) tea.Cmd { return m.setDecision("pending") }},
		{ID: "findings.next-pending", Cat: CatFindings, Name: "Next pending finding",
			Desc: "jump to the next pending finding", Keys: []string{"gp"},
			When: hasFinding, Run: func(m *model) tea.Cmd { m.jumpPending(1); return nil }},
		{ID: "findings.prev-pending", Cat: CatFindings, Name: "Previous pending finding",
			Desc: "jump to the previous pending finding", Keys: []string{"gP"},
			When: hasFinding, Run: func(m *model) tea.Cmd { m.jumpPending(-1); return nil }},
		{ID: "findings.discuss", Cat: CatFindings, Name: "Discuss with AI",
			Desc: "ask the AI agent about this finding", Keys: []string{"d"},
			When: func(m *model) bool {
				return hasFinding(m) && !inCommentsView(m) && !m.busy && m.askingID == ""
			},
			Run: func(m *model) tea.Cmd { return m.startDiscuss() }},
		{ID: "findings.edit-comment", Cat: CatFindings, Name: "Edit suggested comment",
			Desc: "edit the comment text of the selected finding", Keys: []string{"e"},
			When: func(m *model) bool { return hasFinding(m) && !inCommentsView(m) },
			Run:  func(m *model) tea.Cmd { return m.startInlineCommentEdit() }},
		{ID: "findings.open-diff", Cat: CatFindings, Name: "Open location in full diff",
			Desc: "browse the continuous diff at the finding's line", Keys: []string{"gd"},
			When: hasFinding, Run: func(m *model) tea.Cmd { m.browseFinding(); return nil }},
		{ID: "findings.nvim", Cat: CatFindings, Name: "Open in Neovim",
			Desc: "open the file at the finding line (worktree/herdr)", Keys: []string{"v"},
			When: func(m *model) bool { return hasFinding(m) && m.rev.Worktree != "" },
			Run:  func(m *model) tea.Cmd { return m.openNvimCmd() }},
		{ID: "findings.sort", Cat: CatFindings, Name: "Cycle sort",
			Desc: "severity → file → decision", Keys: []string{"o"},
			Run: func(m *model) tea.Cmd { m.cycleSort(); return nil }},
		{ID: "findings.filter", Cat: CatFindings, Name: "Filter findings",
			Desc: "menu: status + severity (f)", Keys: []string{"f"},
			Run: func(m *model) tea.Cmd { m.overlay = ovFilter; return nil }},
		// one palette action per review status — searchable via ":pending",
		// ":approved", … (the f-menu stays for keyboard use)
		{ID: "findings.filter-pending", Cat: CatFindings, Name: "Filter: pending findings",
			Desc: "show only findings awaiting a decision",
			Run:  func(m *model) tea.Cmd { m.setStatusFilter("pending"); return nil }},
		{ID: "findings.filter-approved", Cat: CatFindings, Name: "Filter: approved findings",
			Desc: "show only findings you approved",
			Run:  func(m *model) tea.Cmd { m.setStatusFilter("approved"); return nil }},
		{ID: "findings.filter-rejected", Cat: CatFindings, Name: "Filter: rejected findings",
			Desc: "show only findings you dismissed",
			Run:  func(m *model) tea.Cmd { m.setStatusFilter("rejected"); return nil }},
		{ID: "findings.filter-published", Cat: CatFindings, Name: "Filter: published comments",
			Desc: "show only comments already on GitLab",
			Run:  func(m *model) tea.Cmd { m.setStatusFilter("published"); return nil }},
		{ID: "findings.filter-stale", Cat: CatFindings, Name: "Filter: stale findings",
			Desc: "lines moved since the review (refresh needed)",
			Run:  func(m *model) tea.Cmd { m.setStatusFilter("stale"); return nil }},
		{ID: "findings.filter-all", Cat: CatFindings, Name: "Filter: all findings",
			Desc: "clear the status filter",
			Run:  func(m *model) tea.Cmd { m.setStatusFilter("all"); return nil }},

		// ---- manual review ----
		{ID: "view.findings", Cat: CatManual, Name: "Show findings list",
			Desc: "left pane: AI findings (1)", Keys: []string{"1"},
			Run: func(m *model) tea.Cmd { m.setView(viewFindings); return nil }},
		{ID: "view.files", Cat: CatManual, Name: "Show changed files",
			Desc: "left pane: changed-file tree (2)", Keys: []string{"2"},
			Run: func(m *model) tea.Cmd { m.setView(viewFiles); return nil }},
		{ID: "view.comments", Cat: CatManual, Name: "Show comment navigator",
			Desc: "left pane: all comments (3)", Keys: []string{"3"},
			Run: func(m *model) tea.Cmd { m.setView(viewComments); return nil }},
		{ID: "view.file-findings", Cat: CatManual, Name: "Show file's findings",
			Desc: "left pane: findings for the selected file (4) — set the file with 2 → Enter",
			Keys: []string{"4"},
			When: func(m *model) bool { return m.fileFindingsFile != "" },
			Run:  func(m *model) tea.Cmd { m.setView(viewFileFindings); return nil }},
		{ID: "files.mark-reviewed", Cat: CatManual, Name: "Toggle file reviewed",
			Desc: "mark the selected/browsed file reviewed (m)",
			Keys: []string{"m"},
			When: func(m *model) bool { return m.leftView == viewFiles || hasBrowse(m) },
			Run:  func(m *model) tea.Cmd { m.toggleReviewedFile(); return nil }},
		{ID: "files.next-unreviewed", Cat: CatManual, Name: "Next unreviewed file",
			Desc: "jump to the next file without a reviewed mark (gn)", Keys: []string{"gn"},
			When: func(m *model) bool { return len(m.reviewedSet()) < len(m.changedFiles()) },
			Run:  func(m *model) tea.Cmd { m.nextUnreviewed(); return nil }},
		{ID: "files.mark-hunk", Cat: CatManual, Name: "Toggle hunk reviewed",
			Desc: "mark the hunk at the cursor reviewed (R)", Keys: []string{"R"},
			When: hasBrowse, Run: func(m *model) tea.Cmd { m.toggleReviewedHunk(); return nil }},
		{ID: "files.next", Cat: CatManual, Name: "Next changed file",
			Desc: "jump to the next file in the diff (})", Keys: []string{"}"},
			Run: func(m *model) tea.Cmd { m.fileJump(1); return nil }},
		{ID: "files.prev", Cat: CatManual, Name: "Previous changed file",
			Desc: "jump to the previous file in the diff ({)", Keys: []string{"{"},
			Run: func(m *model) tea.Cmd { m.fileJump(-1); return nil }},

		// ---- comments ----
		{ID: "comments.add", Cat: CatComments, Name: "Add line comment",
			Desc: "comment on the diff cursor / finding line (c)", Keys: []string{"c"},
			When: func(m *model) bool { return hasBrowse(m) || hasFinding(m) },
			Run:  func(m *model) tea.Cmd { return m.startDraftComment() }},
		{ID: "comments.visual", Cat: CatComments, Name: "Start range selection",
			Desc: "select lines for a range comment (V)", Keys: []string{"V"},
			When: hasBrowse, Run: func(m *model) tea.Cmd { m.startVisual(); return nil }},
		{ID: "comments.general", Cat: CatComments, Name: "Add general MR comment",
			Desc: "a comment not attached to a diff line", Keys: nil,
			Run: func(m *model) tea.Cmd { return m.startGeneralComment() }},
		{ID: "comments.edit", Cat: CatComments, Name: "Edit comment",
			Desc: "edit the selected local draft", Keys: []string{"e"},
			When: hasDraftSel, Run: func(m *model) tea.Cmd { return m.editSelectedDraft() }},
		{ID: "comments.delete", Cat: CatComments, Name: "Delete draft",
			Desc: "remove the selected local draft (d, asks to confirm)", Keys: []string{"d"},
			When: hasDraftSel, Run: func(m *model) tea.Cmd { m.confirmDeleteDraft(); return nil }},
		{ID: "comments.toggle-ready", Cat: CatComments, Name: "Toggle ready to publish",
			Desc: "hold the draft back / mark it ready", Keys: nil,
			When: hasDraftSel, Run: func(m *model) tea.Cmd { m.toggleDraftReady(); return nil }},
		{ID: "comments.external-edit", Cat: CatComments, Name: "Edit draft in $EDITOR",
			Desc: "edit the selected draft with an external editor", Keys: nil,
			When: hasDraftSel, Run: func(m *model) tea.Cmd { return m.editDraftExternal() }},

		// ---- navigation ----
		{ID: "nav.down", Cat: CatNavigation, Name: "Down",
			Desc: "next item / scroll down (j)", Keys: []string{"j", "down"},
			Run: func(m *model) tea.Cmd { m.cursorDown(); return nil }},
		{ID: "nav.up", Cat: CatNavigation, Name: "Up",
			Desc: "previous item / scroll up (k)", Keys: []string{"k", "up"},
			Run: func(m *model) tea.Cmd { m.cursorUp(); return nil }},
		{ID: "nav.first", Cat: CatNavigation, Name: "Go to first",
			Desc: "top of the focused view (gg)", Keys: []string{"gg"},
			Run: func(m *model) tea.Cmd { m.toFirst(); return nil }},
		{ID: "nav.last", Cat: CatNavigation, Name: "Go to last",
			Desc: "bottom of the focused view (G)", Keys: []string{"G"},
			Run: func(m *model) tea.Cmd { m.toLast(); return nil }},
		{ID: "nav.half-down", Cat: CatNavigation, Name: "Half page down",
			Desc: "scroll down half a page (Ctrl+d)", Keys: []string{"ctrl+d"},
			Run: func(m *model) tea.Cmd { m.halfScroll(1); return nil }},
		{ID: "nav.half-up", Cat: CatNavigation, Name: "Half page up",
			Desc: "scroll up half a page (Ctrl+u)", Keys: []string{"ctrl+u"},
			Run: func(m *model) tea.Cmd { m.halfScroll(-1); return nil }},
		{ID: "nav.focus-next", Cat: CatNavigation, Name: "Cycle focus",
			Desc: "next panel (Tab)", Keys: []string{"tab"},
			Run: func(m *model) tea.Cmd { m.focusNext(); return nil }},
		{ID: "nav.focus-prev", Cat: CatNavigation, Name: "Cycle focus back",
			Desc: "previous panel (Shift+Tab)", Keys: []string{"shift+tab"},
			Run: func(m *model) tea.Cmd { m.focusPrev(); return nil }},
		{ID: "nav.focus-left", Cat: CatNavigation, Name: "Focus left panel",
			Desc: "focus the navigation pane (Ctrl+h)", Keys: []string{"ctrl+h"},
			Run: func(m *model) tea.Cmd { m.focusPane(-1); return nil }},
		{ID: "nav.focus-right", Cat: CatNavigation, Name: "Focus right panel",
			Desc: "focus the diff/details pane (Ctrl+l)", Keys: []string{"ctrl+l"},
			Run: func(m *model) tea.Cmd { m.focusPane(1); return nil }},
		{ID: "nav.tab-prev", Cat: CatNavigation, Name: "Previous detail tab",
			Desc: "Diff · Explanation · Evidence · Comment · Discussion (h)", Keys: []string{"h"},
			When: func(m *model) bool { return m.focus == focusDetail },
			Run:  func(m *model) tea.Cmd { m.cycleTab(-1); return nil }},
		{ID: "nav.tab-next", Cat: CatNavigation, Name: "Next detail tab",
			Desc: "cycle the detail tabs (l)", Keys: []string{"l"},
			When: func(m *model) bool { return m.focus == focusDetail },
			Run:  func(m *model) tea.Cmd { m.cycleTab(1); return nil }},
		{ID: "nav.hunk-prev", Cat: CatNavigation, Name: "Previous hunk",
			Desc: "jump to the previous hunk ([)", Keys: []string{"["},
			When: func(m *model) bool { return hasBrowse(m) || hasFinding(m) },
			Run:  func(m *model) tea.Cmd { m.hunkJump(-1); return nil }},
		{ID: "nav.hunk-next", Cat: CatNavigation, Name: "Next hunk",
			Desc: "jump to the next hunk (])", Keys: []string{"]"},
			When: func(m *model) bool { return hasBrowse(m) || hasFinding(m) },
			Run:  func(m *model) tea.Cmd { m.hunkJump(1); return nil }},
		{ID: "nav.ctx-more", Cat: CatNavigation, Name: "More diff context",
			Desc: "widen the context window (+)", Keys: []string{"+", "="},
			When: hasFindingWindow, Run: func(m *model) tea.Cmd { m.ctxAdjust(3); return nil }},
		{ID: "nav.ctx-less", Cat: CatNavigation, Name: "Less diff context",
			Desc: "narrow the context window (-)", Keys: []string{"-"},
			When: hasFindingWindow, Run: func(m *model) tea.Cmd { m.ctxAdjust(-3); return nil }},
		{ID: "nav.horiz-left", Cat: CatNavigation, Name: "Scroll left",
			Desc: "long code lines (,)", Keys: []string{","},
			Run: func(m *model) tea.Cmd { m.horizScroll(-24); return nil }},
		{ID: "nav.horiz-right", Cat: CatNavigation, Name: "Scroll right",
			Desc: "long code lines (.)", Keys: []string{"."},
			Run: func(m *model) tea.Cmd { m.horizScroll(24); return nil }},
		{ID: "nav.search", Cat: CatNavigation, Name: "Search",
			Desc: "filter/search the focused view (/)", Keys: []string{"/"},
			Run: func(m *model) tea.Cmd { m.openSearch(); return nil }},
		{ID: "nav.search-next", Cat: CatNavigation, Name: "Next search match",
			Desc: "repeat the search forward (n)", Keys: []string{"n"},
			When: func(m *model) bool { return m.searchText != "" },
			Run:  func(m *model) tea.Cmd { m.searchJump(1); return nil }},
		{ID: "nav.search-prev", Cat: CatNavigation, Name: "Previous search match",
			Desc: "repeat the search backward (N)", Keys: []string{"N"},
			When: func(m *model) bool { return m.searchText != "" },
			Run:  func(m *model) tea.Cmd { m.searchJump(-1); return nil }},
		{ID: "nav.open", Cat: CatNavigation, Name: "Open selection",
			Desc: "details / browse file / jump (Enter)", Keys: []string{"enter"},
			Run: func(m *model) tea.Cmd { return m.openSelection() }},

		// ---- view ----
		{ID: "view.help", Cat: CatView, Name: "Keyboard help",
			Desc: "contextual key reference (?)", Keys: []string{"?"},
			Run: func(m *model) tea.Cmd { m.overlay = ovHelp; return nil }},
		{ID: "view.palette", Cat: CatView, Name: "Command palette",
			Desc: "searchable action list (:)", Keys: []string{":"},
			Run: func(m *model) tea.Cmd { m.openPalette(); return nil }},
		{ID: "view.summary", Cat: CatView, Name: "Review summary",
			Desc: "state of findings, comments and progress (s)", Keys: []string{"s"},
			Run: func(m *model) tea.Cmd { m.overlay = ovSummary; return nil }},
		{ID: "view.summary-edit", Cat: CatView, Name: "Edit summary comment",
			Desc: "$EDITOR on the general review note (S outside the workspace)", Keys: []string{"S"},
			When: func(m *model) bool { return wsPane == nil },
			Run:  func(m *model) tea.Cmd { return m.editSummaryCmd() }},
		{ID: "view.inspect", Cat: CatView, Name: "Inspect finding",
			Desc: "full detail peek without leaving navigation (i)", Keys: []string{"i"},
			When: hasFinding, Run: func(m *model) tea.Cmd { m.overlay = ovInspector; return nil }},
		{ID: "view.copy", Cat: CatView, Name: "Copy to clipboard",
			Desc: "comment / diff line / draft / path (y)", Keys: []string{"y"},
			Run: func(m *model) tea.Cmd { return m.runCopy() }},
		{ID: "view.zoom", Cat: CatView, Name: "Toggle zoom",
			Desc: "maximize the details/diff pane (z)", Keys: []string{"z"},
			Run: func(m *model) tea.Cmd { m.zoom = !m.zoom; m.clampScrolls(); return nil }},
		{ID: "view.prompt", Cat: CatView, Name: "View AI prompt",
			Desc: "the exact prompt sent to the reviewer (p)", Keys: []string{"p"},
			Run: func(m *model) tea.Cmd { return m.viewFileCmd(promptPath(m.dir), "prompt") }},
		{ID: "view.log", Cat: CatView, Name: "View agent log",
			Desc: "raw AI reviewer output (L)", Keys: []string{"L"},
			Run: func(m *model) tea.Cmd { return m.viewFileCmd(logPath(m.dir), "agent log") }},
		{ID: "view.theme", Cat: CatView, Name: "Cycle theme",
			Desc: "switch default ↔ Dracula (T)", Keys: []string{"T"},
			Run: func(m *model) tea.Cmd {
				if err := CycleTheme(); err != nil {
					m.msg = err.Error()
				} else {
					m.msg = "theme: " + ThemeName() + " · icons: " + IconsMode()
				}
				return nil
			}},

		// ---- export ----
		{ID: "export.open", Cat: CatExport, Name: "Export review data",
			Desc: "findings and/or comments to JSON or Markdown (x)", Keys: []string{"x"},
			Run: func(m *model) tea.Cmd { m.openExport(); return nil }},

		// ---- gitlab ----
		{ID: "publish.preview", Cat: CatGitLab, Name: "Publish approved comments",
			Desc: "preview the exact batch, then confirm (P)", Keys: []string{"P"},
			Run: func(m *model) tea.Cmd { return m.publishPreviewCmd() }},
		{ID: "publish.approve-mr", Cat: CatGitLab, Name: "Approve the MR (formal)",
			Desc: "separate action; asks for its own confirmation", Keys: nil,
			Run: func(m *model) tea.Cmd { return m.publishApproveCmd() }},
		{ID: "publish.refresh", Cat: CatGitLab, Name: "Refresh review (MR moved)",
			Desc: "re-diff + re-review, decisions preserved", Keys: nil,
			Run: func(m *model) tea.Cmd { return m.refreshCmd() }},

		{ID: "workspace.open-tuicr", Cat: CatManual, Name: "Open tuicr pane",
			Desc: "embed tuicr beside the cockpit (W) — mouse works", Keys: []string{"W"},
			Run: func(m *model) tea.Cmd { return m.openTuicrPane() }},

		// ---- tuicr staging (approved findings only; local session store) ----
		// S is contextual: while the hybrid workspace is open it ALWAYS
		// routes to staging (with honest feedback when nothing is stageable —
		// a dead key would be worse); outside it stays palette-only.
		{ID: "tuicr.stage", Cat: CatGitLab, Name: "Stage approved findings in tuicr",
			Desc: "insert approved comments into the tuicr review session (S in workspace)", Keys: []string{"S"},
			When: func(m *model) bool { return wsPane != nil || hasStageable(m) },
			Run: func(m *model) tea.Cmd {
				if wsPane != nil && !hasStageable(m) {
					m.msg = "nothing approved to stage (approve with a first)"
					return nil
				}
				return m.stageTuicrCmd()
			}},
		{ID: "tuicr.sync", Cat: CatGitLab, Name: "Sync tuicr staged comments",
			Desc: "detect human edits/deletions of staged comments", Keys: nil,
			Run: func(m *model) tea.Cmd { return m.syncTuicrCmd() }},

		// ---- application ----
		{ID: "app.quit", Cat: CatApp, Name: "Quit",
			Desc: "leave the review (state is saved on every change)", Keys: []string{"q", "ctrl+c"},
			Run: func(m *model) tea.Cmd { return tea.Quit }},
		{ID: "app.clean-worktree", Cat: CatApp, Name: "Remove worktree",
			Desc: "delete the checkout, keep all review state (C)", Keys: []string{"C"},
			When: func(m *model) bool { return m.rev.Worktree != "" },
			Run:  func(m *model) tea.Cmd { m.confirmClean = true; return nil }},
	}
}

// ---- dispatch --------------------------------------------------------------

// hasStageable reports whether any finding is eligible for tuicr staging:
// approved, never published, not stale.
func hasStageable(m *model) bool {
	for i := range m.findings {
		f := &m.findings[i]
		if f.Decision == "approved" && f.Publication == nil && !f.Stale {
			return true
		}
	}
	return false
}

// keyPrefixes lists the first keys of multi-key sequences.
var keyPrefixes = []string{"g"}

// enabledActions returns the currently enabled actions in registry order.
func enabledActions(m *model) []Action {
	all := buildActions(m)
	var out []Action
	for i := range all {
		if all[i].When == nil || all[i].When(m) {
			out = append(out, all[i])
		}
	}
	return out
}

// lookup resolves one key (single or completed two-key sequence) to an
// enabled action.
func (m *model) lookup(key string) *Action {
	all := buildActions(m)
	for i := range all {
		a := &all[i]
		if a.When != nil && !a.When(m) {
			continue
		}
		for _, k := range a.Keys {
			if k == key {
				return a
			}
		}
	}
	return nil
}

// handleKey dispatches a normal-mode key press through the registry.
// Returns (cmd, consumed).
func (m *model) handleKey(key string) tea.Cmd {
	// multi-key sequences: complete or cancel a pending prefix
	if m.pendingKey != "" {
		combo := m.pendingKey + key
		m.pendingKey = ""
		if a := m.lookup(combo); a != nil {
			return m.run(a)
		}
		return nil // unknown sequence: swallowed (vim behaviour)
	}
	for _, p := range keyPrefixes {
		if key == p {
			m.pendingKey = key
			return nil
		}
	}
	if a := m.lookup(key); a != nil {
		return m.run(a)
	}
	m.missKeyFeedback(key)
	return nil
}

// missKeyHints explains the few keys users press in the wrong context —
// unbound keys are otherwise swallowed silently (vi behaviour) and feel
// broken. Genuinely unknown keys stay silent.
var missKeyHints = map[string]string{
	"m": "m reviews the selected file (2 → tree) or hunks in the diff (gd)",
	"R": "R reviews the hunk under the cursor (gd first)",
	"e": "e edits the comment of the selected finding/draft",
}

func (m *model) missKeyFeedback(key string) {
	if hint, ok := missKeyHints[key]; ok {
		m.msg = hint
	}
}

func (m *model) run(a *Action) tea.Cmd {
	if a.When != nil && !a.When(m) {
		m.msg = "unavailable here: " + a.Name
		return nil
	}
	m.msg = "" // movement/feedback handoff
	return a.Run(m)
}

// ---- footer hints ----------------------------------------------------------

// contextHints is the single source for the footer key bar: per context, the
// ordered hints to show. Rendering goes through widget.RenderHints; a test
// resolves every referenced key against the registry so the bar can never
// drift from the real bindings (vi-SQL's derived-footer invariant).
func (m *model) contextHints() []widget.Hint {
	// hybrid workspace: the footer documents the FOCUSED pane's keys
	if wsPane != nil {
		switch {
		case wsExited:
			return []widget.Hint{{Keys: "W", Desc: "reopen tuicr"}, {Keys: "q", Desc: "quit workspace"}}
		case wsFocus == wsFocusTuicr:
			return []widget.Hint{
				{Keys: "hjkl", Desc: "move"}, {Keys: "c", Desc: "comment"}, {Keys: "V", Desc: "range"},
				{Keys: "m/R", Desc: "reviewed"}, {Keys: "/", Desc: "search"},
				{Keys: "mouse", Desc: "click/wheel"},
				{Keys: "Ctrl+O", Desc: "→ cockpit"},
			}
		default:
			return []widget.Hint{
				{Keys: "j/k", Desc: "findings"}, {Keys: "Enter/i", Desc: "details (in strip)"}, {Keys: "a/r", Desc: "approve/reject"}, {Keys: "y", Desc: "copy"},
				{Keys: "d", Desc: "discuss"}, {Keys: "S", Desc: "stage→tuicr"}, {Keys: "1/2/3", Desc: "views"},
				{Keys: "Tab", Desc: "→ tuicr"}, {Keys: ":", Desc: "palette"}, {Keys: "W", Desc: "close pane"},
			}
		}
	}
	switch {
	case m.draftEdit != nil:
		return []widget.Hint{{Keys: "type", Desc: ""}, {Keys: "Ctrl+S", Desc: "save"}, {Keys: "Esc", Desc: "cancel (esc twice discards changes)"}}
	case m.exportDlg != nil:
		return []widget.Hint{{Keys: "↑/↓", Desc: "field"}, {Keys: "enter/space", Desc: "change"}, {Keys: "Ctrl+S", Desc: "export"}, {Keys: "Esc", Desc: "cancel"}}
	case m.paletteOn:
		return []widget.Hint{{Keys: "type", Desc: "to filter"}, {Keys: "↑/↓", Desc: ""}, {Keys: "enter", Desc: "run"}, {Keys: "esc", Desc: "close"}}
	case m.overlay == ovPublish:
		return []widget.Hint{{Keys: "Enter", Desc: "run publisher"}, {Keys: "Esc", Desc: "cancel"}}
	case m.visualOn:
		return []widget.Hint{{Keys: "j/k", Desc: "extend"}, {Keys: "c", Desc: "comment on range"}, {Keys: "esc", Desc: "cancel"}}
	case m.focus == focusDetail:
		return []widget.Hint{{Keys: "j/k", Desc: "scroll"}, {Keys: "h/l", Desc: "tabs"}, {Keys: "[:]", Desc: "hunk"}, {Keys: "c", Desc: "comment"}, {Keys: "i", Desc: "inspect"}, {Keys: "z", Desc: "zoom"}, {Keys: ":", Desc: "commands"}, {Keys: "?", Desc: "help"}}
	case m.leftView == viewFiles:
		return []widget.Hint{{Keys: "j/k", Desc: "file"}, {Keys: "enter", Desc: "browse"}, {Keys: "m", Desc: "reviewed"}, {Keys: "gn", Desc: "next unreviewed"}, {Keys: "/", Desc: "filter"}, {Keys: ":", Desc: "commands"}}
	case m.leftView == viewComments:
		return []widget.Hint{{Keys: "j/k", Desc: "comment"}, {Keys: "enter", Desc: "jump"}, {Keys: "e", Desc: "edit"}, {Keys: "d", Desc: "delete"}, {Keys: "1/2/3", Desc: "views"}, {Keys: ":", Desc: "commands"}}
	default:
		return []widget.Hint{{Keys: "j/k", Desc: "findings"}, {Keys: "a", Desc: "approve"}, {Keys: "r", Desc: "reject"}, {Keys: "e", Desc: "edit"}, {Keys: "d", Desc: "discuss"}, {Keys: "gd", Desc: "full diff"}, {Keys: ":", Desc: "commands"}, {Keys: "?", Desc: "help"}}
	}
}

// footerHints resolves the context table, prefix-pending completions first:
// while a multi-key sequence is pending (e.g. "g"), the bar shows only the
// possible completions (vi-SQL footer behavior).
func (m *model) footerHints() []widget.Hint {
	if m.pendingKey != "" {
		return widget.PrefixHints(m.pendingKey, m.prefixCompletions(m.pendingKey))
	}
	return m.contextHints()
}

// prefixCompletions lists the enabled action keys starting with prefix
// (sorted; duplicates collapsed).
func (m *model) prefixCompletions(prefix string) []string {
	all := buildActions(m)
	seen := map[string]bool{}
	var out []string
	for i := range all {
		a := &all[i]
		if a.When != nil && !a.When(m) {
			continue
		}
		for _, k := range a.Keys {
			if len(k) > len(prefix) && k[:len(prefix)] == prefix && !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}
