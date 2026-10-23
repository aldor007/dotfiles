package tui

import (
	"fmt"
	"strings"

	"mr-review/internal/store"
)

// ---- shared helpers --------------------------------------------------------

// overlayLayer is the canonical entry point for all floating overlays
// (Chunk 4 §7). It wraps overlayBoxRaw with consistent centered chrome.
// All modals (palette, draft editor, help, inspector, export, summary) route
// through here so underlying layout never reflows.
//
// Invariant: when m.topOverlay() != ovNone, all key handling is short-
// circuited to the overlay (curMode() returns modeOverlay/modeAsk/etc. and
// the global key dispatch routes through updatePalette/draftEditor/etc.).
// Underlying state (selection, scroll position, file cursor) is preserved
// until the overlay is closed.
func overlayLayer(content string, w, h int) []string {
	return overlayBoxLines(content, w, h, "")
}

// overlayBoxLines renders content into a bordered box of exactly h lines
// and w cells (the last box row is exactly w cells wide). This is the raw
// material the View compositor places over the unchanged body frame.
func overlayBoxLines(content string, w, h int, hint string) []string {
	lines := strings.Split(content, "\n")
	lines = append(lines, "", styleDim.Render(hint))
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	inner := w - 2
	out := make([]string, 0, h+2)
	out = append(out, "┌"+strings.Repeat("─", inner)+"┐")
	for _, l := range lines {
		out = append(out, "│"+pad(cut(l, inner), inner)+"│")
	}
	out = append(out, "└"+strings.Repeat("─", inner)+"┘")
	return out
}

// helpFocusGroups lists the action IDs of the context-first section, per
// focused pane. Order is display order. Keys/descs are still rendered FROM
// THE REGISTRY (helpActionLines resolves each ID against enabledActions),
// so this grouping can never drift from the real bindings; registry_test
// asserts every ID exists.
var helpFocusGroups = map[int][]string{
	focusDiff: {"comments.add", "comments.visual", "files.mark-hunk", "files.next", "files.prev",
		"nav.hunk-prev", "nav.hunk-next", "nav.horiz-left", "nav.horiz-right", "view.copy"},
	focusFiles: {"nav.down", "nav.up", "nav.open", "files.mark-reviewed", "files.next-unreviewed",
		"nav.search", "view.files"},
	focusFindings: {"nav.down", "nav.up", "nav.open", "findings.approve", "findings.reject",
		"findings.undo", "findings.next-pending", "findings.prev-pending", "findings.discuss",
		"findings.edit-comment", "findings.open-diff", "findings.sort", "findings.filter",
		"view.inspect", "view.findings-overview"},
}

// helpContextSection picks the context-first ID list for the current focus.
// The comments navigator overrides the findings pane list: there e/d act on
// drafts, not findings.
func (m *model) helpContextSection() []string {
	if m.focus == focusFindings && m.leftView == viewComments {
		return []string{"nav.down", "nav.up", "nav.open", "comments.edit", "comments.delete",
			"comments.toggle-ready", "comments.external-edit", "view.comments"}
	}
	if ids, ok := helpFocusGroups[m.focus]; ok {
		return ids
	}
	return nil
}

// helpActionLines renders registry-derived rows for the given action IDs
// (skipping actions disabled in this context and filter non-matches).
func (m *model) helpActionLines(ids []string, q string, byID map[string]Action) []string {
	match := func(s string) bool { return q == "" || strings.Contains(strings.ToLower(s), q) }
	var out []string
	for _, id := range ids {
		a, ok := byID[id]
		if !ok {
			continue
		}
		if !match(a.ID) && !match(a.Desc) && !match(a.Name) && !match(a.KeyLabel()) {
			continue
		}
		label := a.KeyLabel()
		if label == "" {
			label = "(palette)"
		}
		out = append(out, fmt.Sprintf("  %-12s %s", styleSel.Render(label), a.Desc))
	}
	return out
}

// helpFixedLines renders the MODES & SAFETY section (publish invariant).
func helpFixedLines(q string) []string {
	match := func(s string) bool { return q == "" || strings.Contains(strings.ToLower(s), q) }
	var out []string
	for _, k := range [][]string{
		{"esc", "cancel: selection / search / zoom / browse — never publishes"},
		{":", "command palette (searchable actions, same handlers as keys)"},
		{"P", "publication preview → publisher asks to type PUBLISH"},
		{"1/2/3", "left pane: findings / changed files / comment navigator"},
		{"gc", "comment navigator: your drafts — c new (held) · s stage ready · P publishes"},
		{"i / z", "inspect finding · zoom the details pane"},
	} {
		if !match(k[0]) && !match(k[1]) {
			continue
		}
		out = append(out, fmt.Sprintf("  %-12s %s", styleSel.Render(k[0]), k[1]))
	}
	return out
}

// helpSectionLines builds the scrollable body sections: context-first
// (focused pane), then MODES & SAFETY, then the registry categories.
func (m *model) helpSectionLines(w int) []string {
	q := strings.ToLower(strings.TrimSpace(m.helpFilter))
	acts := enabledActions(m)
	byID := make(map[string]Action, len(acts))
	for _, a := range acts {
		byID[a.ID] = a
	}
	ctxIDs := m.helpContextSection()
	ctx := map[string]bool{}
	for _, id := range ctxIDs {
		ctx[id] = true
	}

	var out []string
	if len(ctxIDs) > 0 {
		out = append(out, styleSection.Render("FOCUSED — "+m.panelLabel()))
		out = append(out, m.helpActionLines(ctxIDs, q, byID)...)
		out = append(out, "")
	}
	out = append(out, styleSection.Render("MODES & SAFETY"))
	out = append(out, helpFixedLines(q)...)
	out = append(out, "")

	matchAction := func(a Action) bool {
		if q == "" {
			return true
		}
		if strings.Contains(strings.ToLower(a.ID), q) || strings.Contains(strings.ToLower(a.Desc), q) ||
			strings.Contains(strings.ToLower(a.Name), q) {
			return true
		}
		for _, kk := range a.Keys {
			if strings.Contains(strings.ToLower(kk), q) {
				return true
			}
		}
		return false
	}
	for _, cat := range categoryOrder {
		var rows []string
		for _, a := range acts {
			if a.ID == "view.help" || ctx[a.ID] || a.Cat != cat || !matchAction(a) {
				continue
			}
			label := a.KeyLabel()
			if label == "" {
				label = "(palette)"
			}
			rows = append(rows, fmt.Sprintf("  %-12s %s", styleSel.Render(label), a.Desc))
		}
		if len(rows) == 0 {
			continue
		}
		out = append(out, styleSection.Render(strings.ToUpper(string(cat))))
		out = append(out, rows...)
		out = append(out, "")
	}
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// helpWindow is the number of scrollable body rows visible at once.
func (m *model) helpWindow() int {
	return max(1, helpMaxH(m.bodyHeight())-5)
}

// helpMaxH is the help box height cap: 80% of the viewport, min 14 rows.
func helpMaxH(h int) int {
	mh := max(14, h*4/5)
	if mh > h-2 {
		mh = h - 2
	}
	return max(10, mh)
}

// helpScrollTo moves the help scroll offset (render clamps to content).
func (m *model) helpScrollTo(delta int) {
	m.helpTop = max(0, m.helpTop+delta)
}

// helpOverlay renders the keyboard reference as a floating box: centered,
// elevated, context-first (focused pane's keys first), then MODES & SAFETY,
// then the registry categories — all derived from the action registry so
// help can never drift from the real bindings. Scrollable (j/k/Ctrl-d/u)
// when the content exceeds the box; `/` live-filters (helpFilterInput).
func (m *model) helpOverlay(w, h int) []string {
	boxW := min(max(60, w-6), 100)
	if boxW > w-2 {
		boxW = max(20, w-2)
	}
	secs := m.helpSectionLines(editorInnerW(boxW))
	win := m.helpWindow()
	maxTop := max(0, len(secs)-win)
	top := min(max(0, m.helpTop), maxTop)

	title := styleHeader.Render("KEYBOARD REFERENCE — FOCUSED: "+m.panelLabel()) +
		styleDim.Render("  ·  / filter · esc close")
	filterLine := ""
	if m.helpFilterOn {
		filterLine = styleDim.Render("filter: ") + m.helpFilterInput.View()
	}
	var b strings.Builder
	b.WriteString(title + "\n")
	b.WriteString(filterLine + "\n")
	end := min(len(secs), top+win)
	for _, l := range secs[top:end] {
		b.WriteString(cut(l, boxW-2) + "\n")
	}
	content := strings.TrimRight(b.String(), "\n")
	boxH := min(helpMaxH(h), len(strings.Split(content, "\n"))+2)
	hint := "/ filter · esc close"
	if maxTop > 0 {
		hint = "j/k scroll · ctrl+d/u page · / filter · esc close"
	}
	return overlayBoxLines(content, boxW, boxH, hint)
}

// publishPreviewOverlay shows exactly what P would hand to the publisher:
// approved AI findings and ready manual comments, each with its destination.
func (m *model) publishPreviewOverlay(w, h int) []string {
	var approved []store.Finding
	for i := range m.findings {
		f := &m.findings[i]
		if f.Decision == "approved" && f.Publication == nil && !f.Stale {
			approved = append(approved, *f)
		}
	}
	var ready []store.DraftComment
	for i := range m.comments {
		c := &m.comments[i]
		if c.State == store.CommentReady && c.Publication == nil {
			ready = append(ready, *c)
		}
	}
	var b strings.Builder
	b.WriteString(styleSection.Render("PUBLICATION PREVIEW — nothing is sent yet") + "\n\n")
	fmt.Fprintf(&b, "target: !%d %s on %s\n", m.rev.IID, m.rev.Project, m.rev.Host)
	fmt.Fprintf(&b, "reviewed head: %s (publisher re-verifies before posting)\n\n", short(m.rev.HeadSHA))
	if stale := countStale(m.findings); stale > 0 {
		b.WriteString(styleWarn.Render(fmt.Sprintf("⚠ %d stale finding(s) — excluded from this batch\n\n", stale)))
	}
	if len(approved) == 0 && len(ready) == 0 {
		b.WriteString(styleWarn.Render("nothing to publish: approve findings or write comments first") + "\n")
	}
	if len(approved) > 0 {
		b.WriteString(styleSection.Render(fmt.Sprintf("AI findings (%d)", len(approved))) + "\n")
	}
	for _, f := range approved {
		fmt.Fprintf(&b, "%s %s %s:%d\n", sevStyle(f.Severity).Render(f.Severity), f.ID, f.File, f.Line)
		for i, l := range wrap(f.EffectiveComment(), w-4) {
			if i >= 2 {
				b.WriteString(styleDim.Render("    … (full text in the Comment tab)\n"))
				break
			}
			b.WriteString("    " + l + "\n")
		}
		b.WriteString("\n")
	}
	if len(ready) > 0 {
		b.WriteString(styleSection.Render(fmt.Sprintf("Manual comments (%d)", len(ready))) + "\n")
	}
	for _, c := range ready {
		loc := c.Location()
		if c.General {
			loc = "(general MR comment)"
		}
		fmt.Fprintf(&b, "%s %s %s\n", styleOK.Render(""), c.ID, loc)
		for i, l := range wrap(c.Text, w-4) {
			if i >= 2 {
				b.WriteString(styleDim.Render("    … (full text in the comment navigator)\n"))
				break
			}
			b.WriteString("    " + l + "\n")
		}
		b.WriteString("\n")
	}
	total := len(approved) + len(ready)
	for _, l := range wrap(fmt.Sprintf("%d comment(s) would be posted — the publisher asks for a final PUBLISH confirmation", total), w-2) {
		b.WriteString(cut(styleWarn.Render(l), w) + "\n")
	}
	boxW := min(w-6, 92)
	boxH := min(h-4, 28)
	return overlayLayer(b.String(), boxW, boxH)
}

func countStale(fs []store.Finding) int {
	n := 0
	for i := range fs {
		if fs[i].Stale {
			n++
		}
	}
	return n
}

// inspectorView renders the quick finding inspection overlay.
func (m *model) inspectorView(w, h int) []string {
	f := m.current()
	if f == nil {
		return overlayLayer(styleDim.Render("Pick a finding — j/k to navigate, enter for details.")+"\n", min(w-6, 80), min(h-4, 8))
	}
	// big floating window: ~90% of the viewport. Content = the DIFF around
	// the finding's location (the comment's place) + the formatted details
	// region, one scrollable document, actions footer pinned at the bottom.
	// Full area: the finding gets the whole terminal (Enter/i semantics).
	boxW := max(60, w-2)
	boxH := max(16, h-2)
	inner := boxW - 2
	bodyH := boxH - 2

	m.syncFindingDetails(f) // scroll resets when the selection changes
	content := append(m.inspectorDiffRows(f, inner), m.findingDetailsContent(f, inner)...)
	foot := m.findingActionRows(inner)
	if bodyH <= len(foot) {
		bodyH = 1
		foot = foot[:1]
	} else {
		bodyH -= len(foot)
	}
	maxTop := max(0, len(content)-bodyH)
	top := m.detailTop[fdScrollKey]
	if top > maxTop {
		top = maxTop
	}
	if top < 0 {
		top = 0
	}
	m.detailTop[fdScrollKey] = top

	rows := make([]string, 0, bodyH+len(foot))
	for i := top; i < top+bodyH; i++ {
		if i < len(content) {
			rows = append(rows, cut(content[i], inner))
		} else {
			rows = append(rows, "")
		}
	}
	rows = append(rows, foot...)
	return overlayLayer(strings.Join(rows, "\n"), boxW, boxH)
}

// inspectorDiffRows renders the diff window around the finding's location
// (the place the comment anchors to), syntax-highlighted with the finding's
// own row highlighted.
func (m *model) inspectorDiffRows(f *store.Finding, w int) []string {
	out := []string{styleSection.Render("DIFF — " + smartPath(f.File, max(10, w-24)) +
		fmt.Sprintf(":%d · %s side", f.Line, f.Side))}
	fd := m.diffFor(f)
	if fd == nil {
		return append(out, styleDim.Render("(diff not in the changed set — file may be unmodified)"))
	}
	rows, hl := DiffWindow(fd, f.Side, f.Line, f.LineEnd, 4)
	const maxDiff = 18
	if len(rows) > maxDiff {
		rows = rows[:maxDiff]
	}
	texts := make([]string, len(rows))
	for i, r := range rows {
		texts[i] = expandTabs(r.Text)
	}
	hlTxt := HighlightedCodeLines(f.File, texts, plainMode())
	for i, r := range rows {
		if r.Kind == DiffHunkHeader {
			out = append(out, cut(styleSel.Render(r.Text), w))
			continue
		}
		marker, col := " ", ""
		switch r.Kind {
		case DiffAdd:
			marker, col = "+", green
		case DiffDel:
			marker, col = "-", red
		}
		line := fmt.Sprintf("%4s %s%s %s", numStr(r.NewNo), col, marker, hlTxt[i])
		if i == hl {
			line = styleCursor.Render(cut(fmt.Sprintf("%4s %s%s %s", numStr(r.NewNo), col, marker, hlTxt[i]), w))
		}
		out = append(out, cut(line, w))
	}
	return append(out, styleBorder.Render(strings.Repeat("─", max(4, w-2))), "")
}

// draftEditorView lives in commenteditor.go (two-region floating editor).

// paletteView renders the overlay (bordered box, centered).
func (m *model) paletteView(w, h int) []string {
	const maxRows = 12
	boxW := min(w-6, 92)
	boxH := min(h-4, maxRows+4)

	var b strings.Builder
	b.WriteString(styleHeader.Render("ACTIONS") + "\n")
	b.WriteString(styleBorder.Render(strings.Repeat("─", boxW-2)) + "\n")
	if len(m.paletteItems) == 0 {
		b.WriteString(styleDim.Render(" no matching actions") + "\n")
	}
	rows := boxH - 4 // title + rule + input + hint
	top := 0
	if m.paletteSel >= rows {
		top = m.paletteSel - rows + 1
	}
	for i := top; i < len(m.paletteItems) && i < top+rows; i++ {
		a := m.paletteItems[i]
		line := fmt.Sprintf("  %-32s %-38s %s",
			truncPlain(a.Name, 32), truncPlain(a.Desc, 38), a.KeyLabel())
		if plainMode() {
			if i == m.paletteSel {
				line = ">" + line[1:]
			}
		} else if i == m.paletteSel {
			line = styleCursor.Render(pad(line, boxW-4))
		}
		b.WriteString(cut(line, boxW-2) + "\n")
	}
	b.WriteString(styleBorder.Render(strings.Repeat("─", boxW-2)) + "\n")
	b.WriteString(m.paletteInput.View() + "\n")
	b.WriteString(styleDim.Render(" type to filter · ↑/↓ or ctrl+n/p · enter run · esc cancel") + "\n")
	return overlayLayer(b.String(), boxW, boxH)
}

func (m *model) exportDialogView(w, h int) []string {
	d := m.exportDlg
	var b strings.Builder
	b.WriteString(styleHeader.Render("EXPORT REVIEW DATA") + "\n\n")

	row := func(idx int, label, value string, selected bool) string {
		cursor := "  "
		if selected {
			cursor = "> "
		}
		return fmt.Sprintf("%s%-13s %s", cursor, label, value)
	}
	b.WriteString(row(0, "Scope:", scopeChoices[d.scopeIdx].label, d.field == 0) + "\n")
	fmtStr := "JSON (versioned schema)"
	if d.fmtIdx == 1 {
		fmtStr = "Markdown (readable report)"
	}
	b.WriteString(row(1, "Format:", fmtStr, d.field == 1) + "\n")
	b.WriteString(row(2, "Destination:", truncPlain(d.dest.View(), w-28), d.field == 2) + "\n")
	b.WriteString("\n")
	check := func(idx int, label string, on bool) string {
		mark := "[ ]"
		if on {
			mark = "[x]"
		}
		return row(idx, mark, label, d.field == idx)
	}
	b.WriteString(check(3, "Include evidence", d.incEvidence) + "\n")
	b.WriteString(check(4, "Include manual comments", d.incComments) + "\n")
	b.WriteString(check(5, "Include rejected findings", d.incRejected) + "\n")
	b.WriteString(check(6, "Include review progress", d.incProgress) + "\n")
	b.WriteString("\n")
	if d.armOverwrite {
		b.WriteString(styleWarn.Render("  file exists — change the destination or confirm again") + "\n")
	}
	b.WriteString(styleDim.Render("  ↑/↓ move · enter/left-right change · space toggle · ctrl+s export · esc cancel") + "\n")

	boxW := min(w-6, 72)
	boxH := min(h-4, 14)
	return overlayLayer(b.String(), boxW, boxH)
}

// publishQueueView lists everything STAGED for publication — approved
// findings (not yet published, not stale) and ready (staged) user comments —
// plus held drafts dimmed as explicitly not staged. Read-only: publication
// itself stays behind P → publisher confirmation.
func (m *model) publishQueueView(w, h int) []string {
	boxW := max(56, min(w-6, w*7/10))
	boxH := max(12, min(h-3, h*85/100))
	inner := boxW - 2
	var b strings.Builder
	approved, ready, held := 0, 0, 0
	b.WriteString(styleSection.Render("PUBLISH QUEUE — staged for publication") + "\n\n")
	b.WriteString(styleOK.Render(fmt.Sprintf("APPROVED FINDINGS")) + styleDim.Render(" · approve with a") + "\n")
	for i := range m.findings {
		f := &m.findings[i]
		if f.Decision == "approved" && f.Publication == nil && !f.Stale {
			approved++
			fmt.Fprintf(&b, "  %s %s %s\n", sevStyle(f.Severity).Render(f.Severity), f.ID,
				styleDim.Render(smartPath(f.File, inner-18)+fmt.Sprintf(":%d", f.Line)))
			for j, l := range wrap(firstLine(f.EffectiveComment()), inner-4) {
				if j >= 2 {
					b.WriteString(styleDim.Render("      …") + "\n")
					break
				}
				fmt.Fprintf(&b, "      %s\n", styleDim.Render(l))
			}
		}
	}
	if approved == 0 {
		b.WriteString(styleDim.Render("  (none — approve a finding with a)") + "\n")
	}
	b.WriteString("\n" + styleOK.Render("READY COMMENTS (user-created, staged)") + "\n")
	for i := range m.comments {
		c := &m.comments[i]
		if c.State == store.CommentReady && c.Publication == nil {
			ready++
			loc := c.Location()
			if c.General {
				loc = "MR-level"
			}
			fmt.Fprintf(&b, "  ● %s %s\n", c.ID, styleDim.Render(loc))
			for j, l := range wrap(firstLine(c.Text), inner-4) {
				if j >= 2 {
					b.WriteString(styleDim.Render("      …") + "\n")
					break
				}
				fmt.Fprintf(&b, "      %s\n", cut(l, inner-4))
			}
		}
	}
	if ready == 0 {
		b.WriteString(styleDim.Render("  (none — write with c, stage in the comment navigator)") + "\n")
	}
	for i := range m.comments {
		c := &m.comments[i]
		if c.State == store.CommentDraft && c.Publication == nil {
			held++
		}
	}
	if held > 0 {
		b.WriteString("\n" + styleDim.Render(fmt.Sprintf("HELD DRAFTS — %d, not staged (toggle in the navigator)", held)) + "\n")
	}
	total := approved + ready
	b.WriteString("\n" + styleBorder.Render(strings.Repeat("─", inner)) + "\n")
	fmt.Fprintf(&b, "%s · %s\n", styleOK.Render(fmt.Sprintf("%d staged item(s)", total)),
		styleDim.Render("P publish (publisher asks PUBLISH) · esc/Q close"))
	if total == 0 {
		b.WriteString(styleWarn.Render("nothing staged yet — approve findings or stage comments") + "\n")
	}
	return overlayLayer(b.String(), boxW, boxH)
}

// viewerView renders a read-only floating text viewer (AI prompt, agent
// log) — p/L used to spawn $EDITOR and yank the user out of the TUI.
func (m *model) viewerView(w, h int) []string {
	boxW := max(60, min(w-6, w*9/10))
	boxH := max(14, min(h-3, h*88/100))
	inner := boxW - 2
	bodyH := boxH - 3
	var b strings.Builder
	b.WriteString(styleSection.Render(m.viewerTitle) + styleDim.Render("  ·  esc close · j/k scroll · Ctrl+d/u page") + "\n")
	b.WriteString(styleBorder.Render(strings.Repeat("─", inner)) + "\n")
	top := m.viewerTop
	maxTop := max(0, len(m.viewerLines)-bodyH)
	top = min(max(top, 0), maxTop)
	m.viewerTop = top
	end := min(top+bodyH, len(m.viewerLines))
	for i := top; i < top+bodyH; i++ {
		if i < end {
			b.WriteString(cut(strings.TrimRight(m.viewerLines[i], "\r"), inner) + "\n")
		} else {
			b.WriteString("\n")
		}
	}
	if end < len(m.viewerLines) {
		b.WriteString(styleDim.Render(fmt.Sprintf("… %d more lines", len(m.viewerLines)-end)) + "\n")
	}
	return overlayLayer(b.String(), boxW, boxH)
}
