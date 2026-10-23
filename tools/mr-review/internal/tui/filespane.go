// filespane.go — the FILES pane (lazygit-style): rendering, preview
// semantics and tree interactions.
//
// Preview contract: while the FILES pane has focus, the file under the
// cursor previews in the center continuous diff — but a preview is only
// armed when the cursor MOVES onto a different file (previewPath), so
// re-focusing the pane or scrolling the diff never yanks the center pane
// around. Enter (openSelection) commits: it browses the file and moves
// focus to the diff. Scrolling the diff updates the active-file indicator
// (▸ + selection color) in the tree, distinct from the cursor row.
package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"mr-review/internal/tui/widget"
)

// filesView renders the changed-file tree pane (zero-arg form kept for the
// narrow single-pane branch in view.go; renderFilesPane adapts w/h).
func (m *model) filesView() []string {
	return m.filesPaneRows(m.listWidth(), m.bodyHeight()-2)
}

// filesPaneRows renders the FILES pane body: focus-aware header (with
// active filter + match count), rule, then the visible WINDOW of tree rows
// (widget.List scroll math — hundreds of files stay responsive). The first
// tree row must land on screen row 5: mouse.go hitTest maps item = y-5.
func (m *model) filesPaneRows(w, h int) []string {
	if m.focus == focusFiles && m.leftView == viewFiles {
		m.filesPreviewSync()
	}
	var out []string

	files := m.changedFiles()
	reviewed := len(m.reviewedSet())
	kind, flat := filesFilterAll, false
	if m.tree != nil {
		kind, flat = m.tree.filterKind, m.tree.flat
	}
	full := fmt.Sprintf("CHANGED FILES %d/%d reviewed", reviewed, len(files))
	short := fmt.Sprintf("FILES %d/%d rev", reviewed, len(files))
	switch {
	case m.searchText != "":
		full += fmt.Sprintf(" · /%s (%d)", m.searchText, m.filesFilterMatches())
		short += fmt.Sprintf(" · /%s (%d)", m.searchText, m.filesFilterMatches())
	case kind != filesFilterAll:
		full += fmt.Sprintf(" · filter:%s (%d)", filesFilterNames[kind], m.filesFilterMatches())
		short += fmt.Sprintf(" · filter:%s (%d)", filesFilterNames[kind], m.filesFilterMatches())
	}
	// length-aware: prefer the full header; degrade to the short form (and
	// keep the filter/match segment — it is the actionable info) when the
	// pane is too narrow for both
	hdr := full
	if cellWidth(hdr) > w && cellWidth(short) <= w {
		hdr = short
	}
	if flat {
		hdr += " · flat"
	}
	if m.focus == focusFiles && m.leftView == viewFiles {
		out = append(out, styleBorderF.Render("● "+hdr))
	} else {
		out = append(out, styleDim.Render("○ "+hdr))
	}
	out = append(out, styleBorder.Render(strings.Repeat("─", max(4, w-2))))

	if len(files) == 0 {
		out = append(out, centered("no changed files", w, h-2)...)
		return m.padRows(out, w, h)
	}

	rows := m.treeRows()
	if len(rows) == 0 {
		out = append(out, styleDim.Render(pad(cut("no files match the filter", w), w)))
		return m.padRows(out, w, h)
	}
	// windowed rendering: only the visible slice of rows is rendered
	l := widget.List{Cursor: m.treeCursor, Len: len(rows)}
	top, end := l.Window(h - 2)
	active := m.browseFile // ▸ marker follows the diff cursor's file
	for i := top; i < end && len(out) < h; i++ {
		line := m.renderTreeRow(rows[i], w, active)
		if i == m.treeCursor {
			line = styleCursor.Render(pad(cut(line, w), w))
		}
		out = append(out, line)
	}
	return m.padRows(out, w, h)
}

// filesFilterMatches counts the file rows surviving the active filters.
func (m *model) filesFilterMatches() int {
	if m.tree == nil {
		return 0
	}
	n := 0
	var walk func(nodes []*treeNode)
	walk = func(nodes []*treeNode) {
		for _, nd := range nodes {
			if nd.dir {
				walk(nd.children)
				continue
			}
			if m.tree.kept(nd) {
				n++ // count matches regardless of collapsed-dir visibility
			}
		}
	}
	walk(m.tree.roots)
	return n
}

// rowMetaTier decides how much right-aligned meta fits at pane width w:
// 3 = ±lines + counts, 2 = finding/comment counts, 0 = names only
// (medium-mode compact sidebar).
func rowMetaTier(w int) int {
	switch {
	case w >= 32:
		return 3
	case w >= 24:
		return 2
	default:
		return 0
	}
}

// renderTreeRow renders one row: optional ▸ active marker, git status
// letter (A/M/D), review mark (✓ reviewed / ○ not), name (dir glyph for
// dirs) and right-aligned meta as width allows. Every state is
// shape+color encoded (never color-only).
func (m *model) renderTreeRow(r treeRow, w int, activePath string) string {
	n := r.node
	tier := rowMetaTier(w)
	filterActive := m.searchText != "" || (m.tree != nil && m.tree.filterKind != filesFilterAll)

	if n.dir {
		glyph := icons.TreeClosed
		if n.expanded || filterActive {
			glyph = icons.TreeOpen
		}
		left := widget.IndentPrefix(r.depth) + glyph + " " + n.name + "/"
		var meta []string
		if tier >= 2 && m.tree != nil {
			files, revd, finds, _ := m.tree.dirAggregate(n)
			if files > 0 && revd > 0 {
				meta = append(meta, plainOr(fmt.Sprintf("%d/%d✓", revd, files),
					styleOK.Render(fmt.Sprintf("%d/%d%s", revd, files, icons.Reviewed))))
			}
			if finds > 0 {
				meta = append(meta, plainOr(fmt.Sprintf("%d✦", finds),
					styleWarn.Render(fmt.Sprintf("%d%s", finds, icons.FindingMark))))
			}
		}
		return joinRowMeta(styleSection.Render(cut(left, w)), meta, w)
	}

	mt := fileMeta{}
	if m.tree != nil {
		mt = m.tree.meta[n.path]
	}
	status := string(n.status)
	if status == " " {
		status = "?"
	}
	statusStyle := styleDim
	switch n.status {
	case 'A':
		statusStyle = lipglossGreen()
	case 'D':
		statusStyle = lipglossRed()
	case 'M':
		statusStyle = lipglossYellow()
	}
	mark := plainOr("○", styleDim.Render("○"))
	if mt.reviewed {
		mark = plainOr(icons.Reviewed, styleOK.Render(icons.Reviewed))
	}
	prefix := "  "
	name := n.name
	if activePath != "" && n.path == activePath {
		prefix = styleSel.Render("▸ ")
		name = styleSel.Render(n.name)
	}
	left := prefix + widget.IndentPrefix(r.depth) + statusStyle.Render(status) + " " + mark + " " + name

	var meta []string
	if tier >= 3 && mt.added+mt.deleted > 0 {
		if mt.added > 0 {
			meta = append(meta, lipglossGreen().Render(fmt.Sprintf("+%d", mt.added)))
		}
		if mt.deleted > 0 {
			meta = append(meta, lipglossRed().Render(fmt.Sprintf("-%d", mt.deleted)))
		}
	}
	if tier >= 2 {
		if mt.findings > 0 {
			meta = append(meta, plainOr(fmt.Sprintf("%d✦", mt.findings),
				styleWarn.Render(fmt.Sprintf("%d%s", mt.findings, icons.FindingMark))))
		}
		if mt.comments > 0 {
			meta = append(meta, plainOr(fmt.Sprintf("%d●", mt.comments),
				styleSel.Render(fmt.Sprintf("%d%s", mt.comments, icons.CommentReady))))
		}
	}
	return joinRowMeta(left, meta, w)
}

// dirAggregate sums file state over a dir subtree (reviewed ratio +
// findings) for the dir row meta.
func (ft *fileTree) dirAggregate(n *treeNode) (files, reviewed, findings, comments int) {
	for _, c := range n.children {
		if c.dir {
			f, r, fi, co := ft.dirAggregate(c)
			files, reviewed, findings, comments = files+f, reviewed+r, findings+fi, comments+co
			continue
		}
		mt := ft.meta[c.path]
		if mt.status == 'D' {
			// deleted files leave nothing left to review: keep them out of
			// the x/y✓ reviewed aggregate (findings/comments still count)
			findings += mt.findings
			comments += mt.comments
			continue
		}
		files++
		if mt.reviewed {
			reviewed++
		}
		findings += mt.findings
		comments += mt.comments
	}
	return files, reviewed, findings, comments
}

// joinRowMeta renders left + right-aligned meta parts, dropping trailing
// parts when the pane is too narrow (never mid-glyph truncation).
func joinRowMeta(left string, meta []string, w int) string {
	for len(meta) > 0 {
		mw := 0
		for _, p := range meta {
			mw += lipgloss.Width(p)
		}
		mw += len(meta) - 1
		if lipgloss.Width(left)+1+mw <= w {
			return left + strings.Repeat(" ", max(1, w-lipgloss.Width(left)-mw)) + strings.Join(meta, " ")
		}
		meta = meta[:len(meta)-1]
	}
	return left
}

// ---- preview semantics -----------------------------------------------------

// filesPreviewSync shows the file under the FILES cursor in the center
// continuous diff — ONLY while the FILES pane is focused, and only when the
// cursor moved onto a different file than the last preview (previewPath),
// so re-focusing the pane or diff scrolling never fights the user. Never
// steals focus; Enter (openSelection → browseFilePath) commits.
// filesPreviewSync arms the diff preview ONLY when nothing is previewed yet
// (startup/view-entry with a focused files pane). Once a preview exists,
// changing it requires explicit cursor movement (filesPreviewFollow) — a
// re-render or a direct treeCursor assignment must never yank the user's
// diff location.
func (m *model) filesPreviewSync() {
	if m.browseFile != "" || m.leftView != viewFiles || m.focus != focusFiles || m.tree == nil {
		return
	}
	r := m.treeRowAt(m.treeCursor)
	if r == nil || r.node.dir {
		return
	}
	m.ensureDiff()
	if m.diffParsed[r.node.path] == nil {
		return // don't claim the preview: a later follow must still be able to arm it
	}
	m.tree.previewPath = r.node.path
	m.diffCursor = m.firstAddressableRow(r.node.path)
	m.syncBrowseFile()
	m.ensureBrowseVisible()
}

// filesPreviewFollow moves the diff to the file the tree cursor just moved
// onto (explicit movement only — never called from render paths).
func (m *model) filesPreviewFollow() {
	if m.leftView != viewFiles || m.tree == nil {
		return
	}
	r := m.treeRowAt(m.treeCursor)
	if r == nil || r.node.dir || r.node.path == m.tree.previewPath {
		return
	}
	m.ensureDiff()
	if m.diffParsed[r.node.path] == nil {
		return // don't claim the preview: a later follow must still be able to arm it
	}
	m.tree.previewPath = r.node.path
	m.diffCursor = m.firstAddressableRow(r.node.path)
	m.syncBrowseFile()
	m.ensureBrowseVisible()
}

// filesPreviewPath reports the file the tree cursor currently previews
// ("" for dirs / empty trees) — the active-file indicator contract.
func (m *model) filesPreviewPath() string {
	if m.tree == nil {
		return ""
	}
	if r := m.treeRowAt(m.treeCursor); r != nil && !r.node.dir {
		return r.node.path
	}
	return ""
}

// ---- tree interactions (registry actions call these) ------------------------

// filesRowForPath returns the visible row index of a file/dir path (-1 when
// hidden or absent).
func (m *model) filesRowForPath(path string) int {
	rows := m.treeRows()
	for i := range rows {
		if rows[i].node.path == path {
			return i
		}
	}
	return -1
}

// filesToggleDir expands/collapses the dir under the cursor (same effect as
// Enter/click on a dir row through openSelection/mouse).
func (m *model) filesToggleDir() {
	if r := m.treeRowAt(m.treeCursor); r != nil && r.node.dir {
		r.node.expanded = !r.node.expanded
		m.rebuildTreeRows()
	}
}

// filesCollapseOrParent implements h: collapse the expanded dir under the
// cursor; from a file, fold the dir it lives in and select it; from an
// already-collapsed dir, climb to the parent dir row (root stays). Folding
// on the way up means repeated h walks out of the tree one level at a time.
func (m *model) filesCollapseOrParent() {
	r := m.treeRowAt(m.treeCursor)
	if r == nil {
		return
	}
	if r.node.dir && r.node.expanded {
		r.node.expanded = false
		m.rebuildTreeRows()
		return
	}
	parent := filepath.Dir(r.node.path)
	if parent == "." || parent == r.node.path {
		return
	}
	if !r.node.dir { // leaving a file: fold the dir it lives in
		if pi := m.filesRowForPath(parent); pi >= 0 {
			if pr := m.treeRowAt(pi); pr != nil && pr.node.dir && pr.node.expanded {
				pr.node.expanded = false
				m.rebuildTreeRows()
			}
		}
	}
	if i := m.filesRowForPath(parent); i >= 0 {
		m.treeCursor = i
	}
}

// filesExpandOrOpen implements l: expand a collapsed dir; open a file in
// the diff (browseFilePath focuses the diff pane, Enter semantics).
func (m *model) filesExpandOrOpen() {
	r := m.treeRowAt(m.treeCursor)
	if r == nil {
		return
	}
	if r.node.dir {
		if !r.node.expanded {
			r.node.expanded = true
			m.rebuildTreeRows()
		}
		return
	}
	m.browseFilePath(r.node.path)
}

// filesSetExpansion expands (on) or collapses (off) every directory. The
// state lands in expandedDirs so it survives rebuilds.
func (m *model) filesSetExpansion(on bool) {
	if m.tree == nil {
		m.buildTree()
	}
	var walk func([]*treeNode)
	walk = func(ns []*treeNode) {
		for _, n := range ns {
			if n.dir {
				n.expanded = on
				walk(n.children)
			}
		}
	}
	walk(m.tree.roots)
	m.rebuildTreeRows()
	if on {
		m.msg = "files: all directories expanded"
	} else {
		m.msg = "files: all directories collapsed"
	}
}

// filesToggleFlat switches between the hierarchy and a flat file list
// (visible rows keep their selection).
func (m *model) filesToggleFlat() {
	if m.tree == nil {
		m.buildTree()
	}
	m.tree.flat = !m.tree.flat
	m.rebuildTreeRows()
	if m.tree.flat {
		m.msg = "files: flat list"
	} else {
		m.msg = "files: tree"
	}
}

// filesCycleSort cycles the files sort mode (diff order → path → review
// status → findings), preserving the selection. No auto-reorder happens on
// new findings unless the findings mode is explicitly selected.
func (m *model) filesCycleSort() {
	if m.tree == nil {
		m.buildTree()
	}
	m.tree.sortMode = (m.tree.sortMode + 1) % filesSortCount
	m.rebuildTreeRows()
	m.msg = "files sort: " + filesSortNames[m.tree.sortMode]
}

// filesSetFilter applies a path filter kind (filesFilter*), preserving
// review state, expansion and the selection when still visible.
func (m *model) filesSetFilter(kind int) {
	if kind < 0 || kind >= filesFilterCount {
		return
	}
	if m.tree == nil {
		m.buildTree()
	}
	m.tree.filterKind = kind
	m.rebuildTreeRows()
	name := filesFilterNames[kind]
	if kind == filesFilterAll {
		m.msg = "files filter: all"
		return
	}
	m.msg = fmt.Sprintf("files filter: %s (%d)", name, m.filesFilterMatches())
}

// filesCycleFilter steps through the filter kinds (all → unreviewed → …).
func (m *model) filesCycleFilter() {
	kind := filesFilterAll
	if m.tree != nil {
		kind = (m.tree.filterKind + 1) % filesFilterCount
	}
	m.filesSetFilter(kind)
}
